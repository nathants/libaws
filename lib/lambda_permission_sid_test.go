package lib

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
)

type lambdaPermissionTransport func(*http.Request) (*http.Response, error)

func (transport lambdaPermissionTransport) Do(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestLambdaPermissionSIDsPreserveDistinctGrants(t *testing.T) {
	const functionARN = "arn:aws:lambda:us-west-2:123456789012:function:function"
	oldClient, oldSession := lambdaClient, sess
	t.Cleanup(func() { lambdaClient, sess = oldClient, oldSession })
	sess = &aws.Config{Region: "us-west-2"}
	statements := map[string]IamStatementEntry{
		"obsolete-format": {Sid: "obsolete-format", Effect: "Allow", Action: "lambda:InvokeFunction", Resource: functionARN},
	}
	mutations := 0
	lambdaClient = lambda.NewFromConfig(aws.Config{
		Region: "us-west-2", Credentials: aws.AnonymousCredentials{}, RetryMaxAttempts: 1,
		HTTPClient: lambdaPermissionTransport(func(request *http.Request) (*http.Response, error) {
			status, body := 200, ""
			switch {
			case request.Method == "GET" && strings.HasSuffix(request.URL.Path, "/policy"):
				doc := IamPolicyDocument{Version: "2012-10-17"}
				for _, statement := range statements {
					doc.Statement = append(doc.Statement, statement)
				}
				body = Json(map[string]string{"Policy": Json(doc)})
			case request.Method == "GET" && strings.HasSuffix(request.URL.Path, "/functions/function"):
				body = `{"Configuration":{"FunctionArn":"` + functionARN + `"}}`
			case request.Method == "DELETE" && strings.Contains(request.URL.Path, "/policy/"):
				mutations++
				delete(statements, Last(strings.Split(request.URL.Path, "/")))
				status = 204
			case request.Method == "POST" && strings.HasSuffix(request.URL.Path, "/policy"):
				var input lambda.AddPermissionInput
				if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
					return nil, err
				}
				mutations++
				sid := aws.ToString(input.StatementId)
				statements[sid] = IamStatementEntry{
					Sid: sid, Effect: "Allow", Action: aws.ToString(input.Action), Resource: functionARN,
					Principal: map[string]any{"Service": aws.ToString(input.Principal)},
					Condition: map[string]any{
						"ArnLike":      map[string]any{"AWS:SourceArn": aws.ToString(input.SourceArn)},
						"StringEquals": map[string]any{"AWS:SourceAccount": aws.ToString(input.SourceAccount)},
					},
				}
				body = `{}`
			default:
				return nil, fmt.Errorf("unexpected request: %s %s", request.Method, request.URL)
			}
			return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
		}),
	})
	const alarmARN = "arn:aws:cloudwatch:us-west-2:123456789012:alarm:"
	sources := []struct{ principal, arn string }{
		{lambdaAlarmInvocationPrincipal, alarmARN + "first-alarm"},
		{lambdaAlarmInvocationPrincipal, alarmARN + "first_alarm"},
		{lambdaAlarmInvocationPrincipal, strings.Replace(alarmARN, "us-west-2", "us-east-1", 1) + "first-alarm"},
		{lambdaAlarmInvocationPrincipal, strings.Replace(alarmARN, "123456789012", "210987654321", 1) + "first-alarm"},
		{"events.amazonaws.com", alarmARN + "first-alarm"},
	}
	var sids []string
	for _, source := range sources {
		sid, err := lambdaEnsurePermissionWithSourceAccount(context.Background(), "function", source.principal, source.arn, "123456789012", false)
		if err != nil {
			t.Fatal(err)
		}
		sids = append(sids, sid)
	}
	if len(statements) != len(sources)+1 {
		t.Fatalf("distinct source permissions collided: got %d statements, want %d", len(statements), len(sources)+1)
	}
	before := mutations
	for index, source := range sources {
		sid, err := lambdaEnsurePermissionWithSourceAccount(context.Background(), "function", source.principal, source.arn, "123456789012", false)
		if err != nil || sid != sids[index] {
			t.Fatalf("permission ID not stable: sid=%s err=%v", sid, err)
		}
	}
	if mutations != before {
		t.Fatal("converged grants were rewritten")
	}
	if err := lambdaRemoveUnusedPermissions(context.Background(), "function", sids, true); err != nil || len(statements) != len(sources)+1 {
		t.Fatalf("preview changed grants: %v", err)
	}
	if err := lambdaRemoveUnusedPermissions(context.Background(), "function", sids, false); err != nil || len(statements) != len(sources) {
		t.Fatalf("normal reconciliation did not remove obsolete IDs: %v", err)
	}
	if err := lambdaRemoveUnusedPermissions(context.Background(), "function", nil, false); err != nil || len(statements) != 0 {
		t.Fatalf("permission teardown left grants: %v", err)
	}
}

func TestLambdaPermissionSIDFitsSupportedAlarmNames(t *testing.T) {
	name := strings.Repeat("a", 255)
	config, err := parseLambdaAlarmTrigger(&InfraTrigger{Type: lambdaTriggerAlarm, Attr: []string{"name=" + name, "lambda-invocations=function", "at-least=1/minute"}})
	if err != nil {
		t.Fatal(err)
	}
	sid := lambdaPermissionSID(lambdaAlarmInvocationPrincipal, cloudwatchAlarmARN("aws", "us-west-2", "123456789012", config.name))
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`).MatchString(sid) {
		t.Fatalf("supported alarm name produces invalid %d-byte StatementId: %s", len(sid), sid)
	}
}
