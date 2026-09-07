package lib

import (
	"context"
	"encoding/json"
	"errors"
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
				updates, reads := 0, 0
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
							reads++
							if updates > 0 {
								mapping["State"] = "Enabled"
							}
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
				wantReads := 0
				if !preview {
					wantReads = 1 + want // Settle before reconciliation; verify Enabled after updating.
				}
				if updates != want || reads != wantReads {
					t.Fatalf("updates=%d reads=%d, want updates=%d reads=%d", updates, reads, want, wantReads)
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
	function := &InfraLambda{Name: functionName, Arn: aws.ToString(mapping.FunctionArn), Trigger: []*InfraTrigger{{Type: lambdaTriggerSQS, Attr: []string{queueName, "batch=1", "window=1"}}}}
	t.Logf("mapping state immediately after initial ensure: %s", aws.ToString(mapping.State))
	// Re-ensure directly, without a fixture waiter hiding the Creating transition.
	if err := LambdaEnsureTriggerSQS(ctx, function, false); err != nil {
		t.Fatalf("re-ensure newly created mapping: %v", err)
	}
	ready, err := LambdaClient().GetEventSourceMapping(ctx, &lambda.GetEventSourceMappingInput{UUID: mapping.UUID})
	if err != nil || aws.ToString(ready.State) != "Enabled" || aws.ToString(ready.UUID) != aws.ToString(mapping.UUID) {
		t.Fatalf("new mapping did not settle during re-ensure: out=%v err=%v", ready, err)
	}
	if _, err := LambdaClient().UpdateEventSourceMapping(ctx, &lambda.UpdateEventSourceMappingInput{UUID: mapping.UUID, Enabled: aws.Bool(false)}); err != nil {
		t.Fatal(err)
	}
	current, err := lambdaWaitEventSourceMappingSettled(ctx, LambdaClient(), aws.ToString(mapping.UUID))
	if err != nil || current == nil || aws.ToString(current.state) != "Disabled" {
		t.Fatalf("disable fixture: mapping=%v err=%v", current, err)
	}
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

func TestLambdaSQSSettlesMappingsBeforeReconciliation(t *testing.T) {
	for _, test := range []struct {
		state, settled   string
		readError        bool
		cancel           bool
		refresh          bool
		updates, creates int
	}{
		{state: "Creating", settled: "Enabled"},
		{state: "Enabling", settled: "Enabled"},
		{state: "Disabling", settled: "Disabled", updates: 1},
		{state: "Updating", settled: "Enabled"},
		{state: "Enabled", settled: "Enabled", refresh: true, updates: 1},
		{state: "Deleting", creates: 1},
		{state: "Updating", readError: true},
		{state: "Creating", cancel: true},
	} {
		t.Run(fmt.Sprintf("%s/refresh=%v/error=%v/cancel=%v", test.state, test.refresh, test.readError, test.cancel), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			oldClient, oldSession, oldAccount := lambdaClient, sess, stsAccount
			t.Cleanup(func() { lambdaClient, sess, stsAccount = oldClient, oldSession, oldAccount })
			sess, stsAccount = &aws.Config{Region: "us-west-2"}, aws.String("123456789012")
			const functionARN = "arn:aws:lambda:us-west-2:123456789012:function:function"
			const sourceARN = "arn:aws:sqs:us-west-2:123456789012:queue"
			reads, updates, creates := 0, 0, 0
			lambdaClient = lambda.NewFromConfig(aws.Config{
				Region: "us-west-2", Credentials: aws.AnonymousCredentials{}, RetryMaxAttempts: 1,
				HTTPClient: lambdaSQSTransport(func(request *http.Request) (*http.Response, error) {
					mapping := map[string]any{
						"UUID": "fixture", "FunctionArn": functionARN, "EventSourceArn": sourceARN,
						"State": test.state, "BatchSize": 10, "MaximumBatchingWindowInSeconds": 0,
					}
					status, body := 200, ""
					switch {
					case request.Method == "GET" && strings.HasSuffix(request.URL.Path, "/event-source-mappings"):
						body = Json(map[string]any{"EventSourceMappings": []any{mapping}})
					case request.Method == "GET" && strings.HasSuffix(request.URL.Path, "/event-source-mappings/fixture"):
						reads++
						if test.cancel {
							cancel()
							body = Json(mapping)
						} else if test.readError {
							status, body = 403, `{"__type":"AccessDeniedException","message":"settling read denied"}`
						} else if test.settled == "" {
							status, body = 404, `{"__type":"ResourceNotFoundException","message":"mapping deletion completed"}`
						} else {
							mapping["State"] = test.settled
							if test.refresh && updates == 0 {
								mapping["BatchSize"], mapping["MaximumBatchingWindowInSeconds"] = 2, 2
							}
							if updates > 0 {
								mapping["State"] = "Enabled"
							}
							body = Json(mapping)
						}
					case request.Method == "PUT":
						if reads == 0 {
							status, body = 400, `{"__type":"ResourceInUseException","message":"cannot update an in-flight mapping"}`
							break
						}
						var input lambda.UpdateEventSourceMappingInput
						if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
							return nil, err
						}
						if aws.ToString(input.FunctionName) != "function" || (test.settled == "Disabled" && !aws.ToBool(input.Enabled)) ||
							(test.refresh && (aws.ToInt32(input.BatchSize) != 10 || input.MaximumBatchingWindowInSeconds == nil || *input.MaximumBatchingWindowInSeconds != 0)) {
							return nil, fmt.Errorf("update did not use settled configuration: %s", Json(input))
						}
						updates++
						body = `{"UUID":"fixture","State":"Updating"}`
					case request.Method == "POST" && strings.HasSuffix(request.URL.Path, "/event-source-mappings"):
						if test.creates == 0 || reads == 0 {
							return nil, fmt.Errorf("unexpected mapping create before deletion settled")
						}
						var input lambda.CreateEventSourceMappingInput
						if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
							return nil, err
						}
						if aws.ToString(input.FunctionName) != "function" || aws.ToString(input.EventSourceArn) != sourceARN || !aws.ToBool(input.Enabled) {
							return nil, fmt.Errorf("incorrect replacement mapping: %s", Json(input))
						}
						creates++
						body = `{"UUID":"replacement","State":"Creating"}`
					default:
						return nil, fmt.Errorf("unexpected request: %s %s", request.Method, request.URL)
					}
					return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
				}),
			})
			function := &InfraLambda{Name: "function", Arn: functionARN, Trigger: []*InfraTrigger{{Type: lambdaTriggerSQS, Attr: []string{"queue"}}}}
			err := LambdaEnsureTriggerSQS(ctx, function, false)
			if test.cancel {
				if !errors.Is(err, context.Canceled) || updates != 0 || creates != 0 {
					t.Fatalf("canceled settling mutated mapping or lost cancellation: updates=%d creates=%d err=%v", updates, creates, err)
				}
			} else if test.readError {
				if err == nil || !strings.Contains(err.Error(), "settling read denied") || updates != 0 || creates != 0 {
					t.Fatalf("settling failure was lost or mutated mapping: updates=%d creates=%d err=%v", updates, creates, err)
				}
			} else if err != nil || updates != test.updates || creates != test.creates {
				t.Fatalf("mapping not converged from settled state: reads=%d updates=%d creates=%d err=%v", reads, updates, creates, err)
			}
			if reads == 0 {
				t.Fatal("mapping was not observed settled before reconciliation")
			}
		})
	}
}
