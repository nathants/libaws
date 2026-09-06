package lib

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
	"github.com/gofrs/uuid"
)

type previewReadTransport func(*http.Request) (*http.Response, error)

func (transport previewReadTransport) Do(request *http.Request) (*http.Response, error) {
	return transport(request)
}

type previewReadCase struct {
	name, action string
	run          func(context.Context, bool) error
}

func previewReadCases(name, functionARN string) []previewReadCase {
	function := &InfraLambda{Name: name, Arn: functionARN}
	return []previewReadCase{
		{"user managed policies", "ListAttachedUserPolicies", func(ctx context.Context, preview bool) error {
			return IamEnsureUserPolicies(ctx, name, nil, preview)
		}},
		{"role managed policies", "ListAttachedRolePolicies", func(ctx context.Context, preview bool) error {
			return IamEnsureRolePolicies(ctx, name, nil, preview)
		}},
		{"role inline policies", "ListRolePolicies", func(ctx context.Context, preview bool) error {
			return IamEnsureRoleAllows(ctx, name, nil, preview)
		}},
		{"user inline policies", "ListUserPolicies", func(ctx context.Context, preview bool) error {
			return IamEnsureUserAllows(ctx, name, nil, preview)
		}},
		{"lambda configuration", "GetFunctionConfiguration", func(ctx context.Context, preview bool) error {
			return lambdaEnsureFunctionConfiguration(ctx, function, nil, 30, 128, preview, false)
		}},
		{"SQS removal", "ListEventSourceMappings", func(ctx context.Context, preview bool) error {
			return LambdaEnsureTriggerSQS(ctx, function, preview)
		}},
	}
}

func TestPreviewReadErrors(t *testing.T) {
	cases := previewReadCases("fixture", "arn:aws:lambda:us-east-1:123456789012:function:fixture")
	cases = append(cases, previewReadCase{"attach role policy", "ListAttachedRolePolicies", func(ctx context.Context, preview bool) error {
		return IamEnsureRolePolicies(ctx, "fixture", []string{"desired"}, preview)
	}})
	for _, test := range cases {
		for _, absent := range []bool{false, true} {
			for _, preview := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/absent=%v/preview=%v", test.name, absent, preview), func(t *testing.T) {
					oldIAM, oldLambda := iamClient, lambdaClient
					t.Cleanup(func() { iamClient, lambdaClient = oldIAM, oldLambda })
					calls := 0
					transport := previewReadTransport(func(request *http.Request) (*http.Response, error) {
						calls++
						status, body := 403, ""
						if strings.HasPrefix(request.URL.Host, "iam.") {
							if err := request.ParseForm(); err != nil {
								return nil, err
							}
							action := request.Form.Get("Action")
							if action == "ListPolicies" && test.name == "attach role policy" {
								status, body = 200, `<ListPoliciesResponse><ListPoliciesResult><Policies><member><PolicyName>desired</PolicyName><Arn>arn:aws:iam::123456789012:policy/desired</Arn></member></Policies></ListPoliciesResult></ListPoliciesResponse>`
							} else {
								if action != test.action {
									return nil, fmt.Errorf("unexpected IAM action %s", action)
								}
								code := "AccessDenied"
								if absent {
									code, status = "NoSuchEntity", 404
								}
								body = `<ErrorResponse><Error><Code>` + code + `</Code><Message>read fixture</Message></Error></ErrorResponse>`
							}
						} else {
							if request.Method != http.MethodGet {
								return nil, fmt.Errorf("unexpected mutation %s", request.Method)
							}
							code := "AccessDeniedException"
							if absent {
								code, status = "ResourceNotFoundException", 404
							}
							body = `{"__type":"` + code + `","message":"read fixture"}`
						}
						return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
					})
					config := aws.Config{Region: "us-east-1", Credentials: aws.AnonymousCredentials{}, HTTPClient: transport, RetryMaxAttempts: 1}
					iamClient, lambdaClient = iam.NewFromConfig(config), lambda.NewFromConfig(config)
					err := test.run(context.Background(), preview)
					if preview && absent {
						if err != nil {
							t.Fatalf("new-resource preview failed: %v", err)
						}
					} else if err == nil || !strings.Contains(err.Error(), "read fixture") {
						t.Fatalf("inventory failure lost: %v", err)
					}
					if calls == 0 {
						t.Fatal("did not read provider")
					}
				})
			}
		}
	}
}

func TestPreviewPreservesPolicyDisappearanceAfterDiscovery(t *testing.T) {
	oldIAM := iamClient
	t.Cleanup(func() { iamClient = oldIAM })
	iamClient = iam.NewFromConfig(aws.Config{
		Region: "us-east-1", Credentials: aws.AnonymousCredentials{}, RetryMaxAttempts: 1,
		HTTPClient: previewReadTransport(func(request *http.Request) (*http.Response, error) {
			if err := request.ParseForm(); err != nil {
				return nil, err
			}
			status, body := 200, ""
			switch action := request.Form.Get("Action"); action {
			case "ListRolePolicies":
				body = `<ListRolePoliciesResponse><ListRolePoliciesResult><PolicyNames><member>existing</member></PolicyNames></ListRolePoliciesResult></ListRolePoliciesResponse>`
			case "GetRolePolicy":
				status, body = 404, `<ErrorResponse><Error><Code>NoSuchEntity</Code><Message>disappeared policy</Message></Error></ErrorResponse>`
			default:
				return nil, fmt.Errorf("unexpected IAM action %s", action)
			}
			return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
		}),
	})
	if err := IamEnsureRoleAllows(context.Background(), "fixture", nil, true); err == nil || !strings.Contains(err.Error(), "disappeared policy") {
		t.Fatalf("configuration disappearance after discovery was suppressed: %v", err)
	}
}

func TestPreviewReadErrorsIntegration(t *testing.T) {
	requireLiveAWSAccount(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	name := "test-preview-" + uuid.Must(uuid.NewV4()).String()
	identity, err := STSClient().GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		t.Fatal(err)
	}
	adminIAM := IamClient()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		_, err := adminIAM.DeleteRole(cleanupCtx, &iam.DeleteRoleInput{RoleName: aws.String(name)})
		var absent *iamtypes.NoSuchEntityException
		if err != nil && !errors.As(err, &absent) {
			t.Errorf("delete preview fixture: %v", err)
			return
		}
		_, err = adminIAM.GetRole(cleanupCtx, &iam.GetRoleInput{RoleName: aws.String(name)})
		if !errors.As(err, &absent) {
			t.Errorf("preview fixture remains or could not verify deletion: %v", err)
		}
	})
	// No policies: this role can read or mutate nothing. Only this caller can assume it.
	role, err := adminIAM.CreateRole(ctx, &iam.CreateRoleInput{
		RoleName:                 aws.String(name),
		AssumeRolePolicyDocument: aws.String(fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":%q},"Action":"sts:AssumeRole"}]}`, aws.ToString(identity.Arn))),
	})
	if err != nil {
		t.Fatal(err)
	}
	var session *sts.AssumeRoleOutput
	if err := Retry(ctx, func() error {
		var err error
		session, err = STSClient().AssumeRole(ctx, &sts.AssumeRoleInput{
			RoleArn: role.Role.Arn, RoleSessionName: aws.String("preview-reads"), DurationSeconds: aws.Int32(900),
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	config := *Session()
	config.Credentials = credentials.NewStaticCredentialsProvider(aws.ToString(session.Credentials.AccessKeyId), aws.ToString(session.Credentials.SecretAccessKey), aws.ToString(session.Credentials.SessionToken))
	oldIAM, oldLambda := iamClient, lambdaClient
	t.Cleanup(func() { iamClient, lambdaClient = oldIAM, oldLambda })
	iamClient, lambdaClient = iam.NewFromConfig(config), lambda.NewFromConfig(config)
	functionARN := "arn:aws:lambda:" + config.Region + ":" + aws.ToString(identity.Account) + ":function:" + name
	for _, test := range previewReadCases(name, functionARN) {
		t.Run(test.name, func(t *testing.T) {
			err := test.run(ctx, true)
			var denied smithy.APIError
			if !errors.As(err, &denied) || (denied.ErrorCode() != "AccessDenied" && denied.ErrorCode() != "AccessDeniedException") {
				t.Fatalf("preview must retain real AWS AccessDenied: %v", err)
			}
		})
	}
	// With full credentials, previews of genuinely absent resources still work.
	iamClient, lambdaClient = adminIAM, lambda.NewFromConfig(*Session())
	for _, test := range previewReadCases(name+"-absent", functionARN+"-absent") {
		t.Run("absent/"+test.name, func(t *testing.T) {
			if err := test.run(ctx, true); err != nil {
				t.Fatal(err)
			}
		})
	}
}
