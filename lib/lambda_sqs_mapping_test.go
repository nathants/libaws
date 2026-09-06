package lib

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
)

type lambdaSQSTransport func(*http.Request) (*http.Response, error)

func (transport lambdaSQSTransport) Do(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestLambdaSQSReenablesDisabledMapping(t *testing.T) {
	for _, state := range []string{"Enabled", "Disabled"} {
		for _, preview := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/preview=%v", state, preview), func(t *testing.T) {
				oldClient, oldSession, oldAccount, oldLogger := lambdaClient, sess, stsAccount, *Logger
				t.Cleanup(func() { lambdaClient, sess, stsAccount, *Logger = oldClient, oldSession, oldAccount, oldLogger })
				sess = &aws.Config{Region: "us-west-2"}
				stsAccount = aws.String("123456789012")
				const functionARN = "arn:aws:lambda:us-west-2:123456789012:function:function"
				updates, enabledReads := 0, 0
				var logs strings.Builder
				Logger.disabled = false
				Logger.Print = func(args ...any) { fmt.Fprint(&logs, args...) }
				lambdaClient = lambda.NewFromConfig(aws.Config{
					Region: "us-west-2", Credentials: aws.AnonymousCredentials{}, RetryMaxAttempts: 1,
					HTTPClient: lambdaSQSTransport(func(request *http.Request) (*http.Response, error) {
						mapping := map[string]any{
							"UUID": "fixture", "FunctionArn": functionARN, "EventSourceArn": "arn:aws:sqs:us-west-2:123456789012:queue",
							"State": state, "BatchSize": 10, "MaximumBatchingWindowInSeconds": 0,
						}
						body := ""
						switch {
						case request.Method == "GET" && strings.HasSuffix(request.URL.Path, "/event-source-mappings"):
							body = Json(map[string]any{"EventSourceMappings": []any{mapping}})
						case !preview && request.Method == "PUT" && strings.HasSuffix(request.URL.Path, "/event-source-mappings/fixture"):
							var input lambda.UpdateEventSourceMappingInput
							if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
								return nil, err
							}
							if !aws.ToBool(input.Enabled) {
								t.Error("update did not enable mapping")
							}
							updates++
							body = `{"UUID":"fixture","State":"Enabling"}`
						case !preview && request.Method == "GET" && strings.HasSuffix(request.URL.Path, "/event-source-mappings/fixture"):
							if updates != 1 {
								return nil, fmt.Errorf("unexpected mapping wait before update")
							}
							enabledReads++
							mapping["State"] = "Enabled"
							body = Json(mapping)
						default:
							return nil, fmt.Errorf("unexpected request: %s %s", request.Method, request.URL)
						}
						return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
					}),
				})
				function := &InfraLambda{Name: "function", Arn: functionARN, Trigger: []*InfraTrigger{{Type: lambdaTriggerSQS, Attr: []string{"queue"}}}}
				if err := LambdaEnsureTriggerSQS(context.Background(), function, preview); err != nil {
					t.Fatal(err)
				}
				if state == "Disabled" && !strings.Contains(logs.String(), "will enable lambda event source mapping") {
					t.Fatalf("disabled mapping accepted as converged: %s", logs.String())
				}
				want := 0
				if state == "Disabled" && !preview {
					want = 1
				}
				if updates != want || enabledReads != want {
					t.Fatalf("updates=%d enabled observations=%d, want %d", updates, enabledReads, want)
				}
			})
		}
	}
}

// The Go SQS example owns provisioning and cleanup, then sends a real message.
func TestLambdaSQSMappingIntegration(t *testing.T) {
	uid := os.Getenv("LIBAWS_SQS_MAPPING_TEST_UID")
	if uid == "" {
		t.Skip("run through examples/simple/go/sqs")
	}
	if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(uid) {
		t.Fatal("requires unique example uid")
	}
	requireLiveAWSAccount(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	functionName, queueName := "test-lambda-"+uid, "test-queue-"+uid
	queueARN, err := SQSArn(ctx, queueName)
	if err != nil {
		t.Fatal(err)
	}
	mappings, err := lambdaListEventSourceMappings(ctx, functionName)
	if err != nil || len(mappings) != 1 || aws.ToString(mappings[0].EventSourceArn) != queueARN {
		t.Fatalf("expected exactly one owned SQS mapping: mappings=%v err=%v", mappings, err)
	}
	mapping := mappings[0]
	if _, err := lambdaWaitEventSourceMappingEnabled(ctx, LambdaClient(), aws.ToString(mapping.UUID)); err != nil {
		t.Fatal(err)
	}
	if _, err := LambdaClient().UpdateEventSourceMapping(ctx, &lambda.UpdateEventSourceMappingInput{UUID: mapping.UUID, Enabled: aws.Bool(false)}); err != nil {
		t.Fatal(err)
	}
	current, err := lambdaWaitEventSourceMappingSettled(ctx, LambdaClient(), aws.ToString(mapping.UUID))
	if err != nil || current == nil || aws.ToString(current.state) != "Disabled" {
		t.Fatalf("disable fixture: mapping=%v err=%v", current, err)
	}
	function := &InfraLambda{Name: functionName, Arn: aws.ToString(mapping.FunctionArn), Trigger: []*InfraTrigger{{Type: lambdaTriggerSQS, Attr: []string{queueName, "batch=1", "window=1"}}}}
	for _, preview := range []bool{true, false} {
		if err := LambdaEnsureTriggerSQS(ctx, function, preview); err != nil {
			t.Fatal(err)
		}
		out, err := LambdaClient().GetEventSourceMapping(ctx, &lambda.GetEventSourceMappingInput{UUID: mapping.UUID})
		want := "Enabled"
		if preview {
			want = "Disabled"
		}
		if err != nil || aws.ToString(out.State) != want || aws.ToString(out.UUID) != aws.ToString(mapping.UUID) {
			t.Fatalf("preview=%v state after ensure: out=%v err=%v", preview, out, err)
		}
	}
	if err := LambdaEnsureTriggerSQS(ctx, function, false); err != nil {
		t.Fatalf("re-ensure enabled mapping: %v", err)
	}
}
