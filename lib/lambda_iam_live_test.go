package lib

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cwlogstypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

type lambdaIAMLiveTransport func(*http.Request) (*http.Response, error)

func (transport lambdaIAMLiveTransport) Do(request *http.Request) (*http.Response, error) {
	return transport(request)
}

// Independently read real IAM at the actual Lambda dispatch boundary. This
// proves API ordering, not instantaneous IAM propagation to every data plane.
func TestLambdaIAMOrderingIntegration(t *testing.T) {
	requireLiveAWSAccount(t)
	t.Setenv(lambdaEnvVarApiID, "")
	t.Setenv(lambdaEnvVarWebsocketID, "")
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()
	name := "test-lambda-iam-" + strings.ToLower(rand.Text()[:16])
	set := "test-set-" + name
	apiName := name + LambdaWebsocketSuffix
	logGroup := "/aws/lambda/" + name
	account, err := StsAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	oldLambda, oldIAM := LambdaClient(), IamClient()
	base := Session().HTTPClient
	if base == nil {
		base = http.DefaultClient
	}
	// Refuse a collision before registering any destructive cleanup.
	if _, err := oldLambda.GetFunction(ctx, &lambda.GetFunctionInput{FunctionName: aws.String(name)}); err == nil {
		t.Fatal("fixture Lambda already exists")
	} else {
		if _, ok := errors.AsType[*lambdatypes.ResourceNotFoundException](err); !ok {
			t.Fatal(err)
		}
	}
	if _, err := oldIAM.GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(name)}); err == nil {
		t.Fatal("fixture role already exists")
	} else {
		if _, ok := errors.AsType[*iamtypes.NoSuchEntityException](err); !ok {
			t.Fatal(err)
		}
	}
	if api, err := Api(ctx, apiName); api != nil || err == nil || err.Error() != ErrApiNotFound {
		t.Fatalf("fixture API absence: %v %v", api, err)
	}
	groups, err := LogsClient().DescribeLogGroups(ctx, &cloudwatchlogs.DescribeLogGroupsInput{LogGroupNamePrefix: aws.String(logGroup)})
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range groups.LogGroups {
		if aws.ToString(group.LogGroupName) == logGroup {
			t.Fatal("fixture log group already exists")
		}
	}
	t.Logf("owned fixture: %s account=%s region=%s", name, account, Region())
	t.Cleanup(func() {
		lambdaClient, iamClient = oldLambda, oldIAM
		cleanup, stop := context.WithTimeout(context.Background(), 3*time.Minute)
		defer stop()
		if err := LambdaDeleteFunction(cleanup, name, false); err != nil {
			t.Error(err)
		}
		if api, err := Api(cleanup, apiName); err == nil {
			if api.Tags[infraSetTagName] != set {
				t.Error("refuse cleanup of unowned API")
			} else if _, err := ApiClient().DeleteApi(cleanup, &apigatewayv2.DeleteApiInput{ApiId: api.ApiId}); err != nil {
				t.Error(err)
			}
		} else if err.Error() != ErrApiNotFound {
			t.Error(err)
		}
		if err := IamDeleteRole(cleanup, name, false); err != nil {
			t.Error(err)
		}
		if _, err := LogsClient().DeleteLogGroup(cleanup, &cloudwatchlogs.DeleteLogGroupInput{LogGroupName: aws.String(logGroup)}); err != nil {
			if _, ok := errors.AsType[*cwlogstypes.ResourceNotFoundException](err); !ok {
				t.Error(err)
			}
		}
		if err := os.Remove(LambdaZipFile(name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Error(err)
		}
		if _, err := oldLambda.GetFunction(cleanup, &lambda.GetFunctionInput{FunctionName: aws.String(name)}); err == nil {
			t.Error("fixture Lambda remains")
		} else {
			if _, ok := errors.AsType[*lambdatypes.ResourceNotFoundException](err); !ok {
				t.Error(err)
			}
		}
		if _, err := oldIAM.GetRole(cleanup, &iam.GetRoleInput{RoleName: aws.String(name)}); err == nil {
			t.Error("fixture role remains")
		} else {
			if _, ok := errors.AsType[*iamtypes.NoSuchEntityException](err); !ok {
				t.Error(err)
			}
		}
		if api, err := Api(cleanup, apiName); api != nil || err == nil || err.Error() != ErrApiNotFound {
			t.Errorf("fixture API remains or is unknown: %v", err)
		}
	})
	oldGrant := &IamAllow{Action: "dynamodb:DescribeTable", Resource: fmt.Sprintf("arn:aws:dynamodb:%s:%s:table/%s-old", Region(), account, name)}
	newGrant := &IamAllow{Action: "dynamodb:DescribeTable", Resource: fmt.Sprintf("arn:aws:dynamodb:%s:%s:table/%s-new", Region(), account, name)}
	apiGrant := fmt.Sprintf("execute-api:ManageConnections arn:aws:execute-api:%s:%s:${WEBSOCKET_ID}/*", Region(), account)
	declaration := &InfraLambda{Name: name, infraSetName: set, dir: t.TempDir(), runtime: lambdaRuntimePython, handler: "main.main", Entrypoint: "main.py", Attr: []string{"timeout=10"}, Policy: []string{"AWSLambdaBasicExecutionRole"}, Allow: []string{oldGrant.String(), apiGrant}, Trigger: []*InfraTrigger{{Type: lambdaTriggerWebsocket}}}
	generation, dispatches := 1, 0
	wantGrants := []*IamAllow{oldGrant}
	wantManaged := []string{"AWSLambdaBasicExecutionRole"}
	lambdaClient = lambda.NewFromConfig(*Session(), func(options *lambda.Options) {
		options.HTTPClient = lambdaIAMLiveTransport(func(request *http.Request) (*http.Response, error) {
			activation := request.Method == "POST" && strings.HasSuffix(request.URL.Path, "/functions") || request.Method == "PUT" && strings.HasSuffix(request.URL.Path, "/functions/"+name+"/code")
			if activation {
				dispatches++
				api, err := Api(request.Context(), apiName)
				if err != nil {
					return nil, err
				}
				grants := append(append([]*IamAllow{}, wantGrants...), &IamAllow{Action: "execute-api:ManageConnections", Resource: fmt.Sprintf("arn:aws:execute-api:%s:%s:%s/*", Region(), account, aws.ToString(api.ApiId))})
				for _, grant := range grants {
					out, err := oldIAM.GetRolePolicy(request.Context(), &iam.GetRolePolicyInput{RoleName: aws.String(name), PolicyName: aws.String(grant.policyName())})
					if err != nil {
						cancel()
						return nil, fmt.Errorf("before real activation, grant absent: %w", err)
					}
					doc, err := url.QueryUnescape(aws.ToString(out.PolicyDocument))
					if err != nil {
						cancel()
						return nil, err
					}
					equal, err := iamPolicyEqual(doc, grant.policyDocument())
					if err != nil {
						cancel()
						return nil, fmt.Errorf("before real activation, grant differs: %w", err)
					}
					if !equal {
						cancel()
						return nil, errors.New("before real activation, grant differs")
					}
				}
				policies, err := IamListRolePolicies(request.Context(), name)
				if err != nil {
					cancel()
					return nil, err
				}
				for _, want := range wantManaged {
					found := false
					for _, policy := range policies {
						found = found || aws.ToString(policy.PolicyName) == want
					}
					if !found {
						cancel()
						return nil, fmt.Errorf("before real activation, managed grant absent: %s", want)
					}
				}
			}
			return base.Do(request)
		})
	})
	prepare := func(declaration *InfraLambda) error {
		if _, err := resetLambdaPackageDir(declaration.Name); err != nil {
			return err
		}
		var data bytes.Buffer
		archive := zip.NewWriter(&data)
		file, err := archive.Create("main.py")
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(file, "def main(event, context):\n    return %d\n", generation); err != nil {
			return err
		}
		if err := archive.Close(); err != nil {
			return err
		}
		return os.WriteFile(LambdaZipFile(declaration.Name), data.Bytes(), 0600)
	}
	if _, err := lambdaEnsureResources(ctx, declaration, false, false, false, nil, prepare); err != nil {
		t.Fatal(err)
	}
	if dispatches != 1 {
		t.Fatalf("create dispatches=%d", dispatches)
	}
	generation = 2
	wantGrants = []*IamAllow{oldGrant, newGrant}
	wantManaged = []string{"AWSLambdaBasicExecutionRole", "AWSXRayDaemonWriteAccess"}
	declaration.Allow, declaration.Policy = []string{newGrant.String(), apiGrant}, []string{"AWSXRayDaemonWriteAccess"}
	if _, err := lambdaEnsureResources(ctx, declaration, false, false, false, nil, prepare); err != nil {
		t.Fatal(err)
	}
	if dispatches != 2 {
		t.Fatalf("create/update dispatches=%d", dispatches)
	}
	if _, err := oldIAM.GetRolePolicy(ctx, &iam.GetRolePolicyInput{RoleName: aws.String(name), PolicyName: aws.String(oldGrant.policyName())}); err == nil {
		t.Fatal("obsolete inline grant was not pruned")
	} else {
		if _, ok := errors.AsType[*iamtypes.NoSuchEntityException](err); !ok {
			t.Fatal(err)
		}
	}
	policies, err := IamListRolePolicies(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	if len(policies) != 1 || aws.ToString(policies[0].PolicyName) != "AWSXRayDaemonWriteAccess" {
		t.Fatal("obsolete managed policy was not pruned")
	}
	invoke, err := oldLambda.Invoke(ctx, &lambda.InvokeInput{FunctionName: aws.String(name), Payload: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if invoke.FunctionError != nil || string(invoke.Payload) != "2" {
		t.Fatalf("actual updated Lambda response: %s error=%v", invoke.Payload, invoke.FunctionError)
	}
	before, err := oldLambda.GetFunction(ctx, &lambda.GetFunctionInput{FunctionName: aws.String(name)})
	if err != nil {
		t.Fatal(err)
	}
	failed, stopFailure := context.WithCancel(ctx)
	defer stopFailure()
	injected := false
	iamClient = iam.NewFromConfig(*Session(), func(options *iam.Options) {
		options.HTTPClient = lambdaIAMLiveTransport(func(request *http.Request) (*http.Response, error) {
			data, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			request.Body = io.NopCloser(bytes.NewReader(data))
			values, err := url.ParseQuery(string(data))
			if err != nil {
				return nil, err
			}
			if values.Get("Action") == "PutRolePolicy" && values.Get("RoleName") == name {
				injected = true
				stopFailure()
				return nil, context.Canceled
			}
			return base.Do(request)
		})
	})
	generation = 3
	declaration.Allow = append(declaration.Allow, oldGrant.String())
	_, err = lambdaEnsureResources(failed, declaration, false, false, false, nil, prepare)
	iamClient = oldIAM
	if err == nil || !injected || dispatches != 2 {
		t.Fatalf("failed prerequisite did not fence real activation: dispatches=%d injected=%t err=%v", dispatches, injected, err)
	}
	after, err := oldLambda.GetFunction(ctx, &lambda.GetFunctionInput{FunctionName: aws.String(name)})
	if err != nil {
		t.Fatal(err)
	}
	if aws.ToString(before.Configuration.CodeSha256) != aws.ToString(after.Configuration.CodeSha256) {
		t.Fatal("failed prerequisites changed deployed code")
	}
	if _, err := oldIAM.GetRolePolicy(ctx, &iam.GetRolePolicyInput{RoleName: aws.String(name), PolicyName: aws.String(newGrant.policyName())}); err != nil {
		t.Fatalf("failed prerequisites lost running code's grant: %v", err)
	}
}
