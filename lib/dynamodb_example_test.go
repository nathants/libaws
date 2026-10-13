package lib

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Run after the example's successful stream copy and any mapping tests. The
// write-failure case removes only that example's output table; its registered
// infrastructure finalizer still removes every remaining resource on failure.
func TestDynamoDBExampleFailures(t *testing.T) {
	uid := os.Getenv("LIBAWS_DYNAMODB_EXAMPLE_UID")
	if uid == "" {
		t.Skip("run through the Go, Python, or Docker DynamoDB example")
	}
	if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(uid) {
		t.Fatal("DynamoDB example requires a unique 12-hex uid")
	}
	requireLiveAWSAccount(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	function, source, target := "test-lambda-"+uid, "test-table-"+uid, "test-other-table-"+uid
	set := "test-infraset-" + uid
	for _, table := range []string{source, target} {
		tags, err := DynamoDBListTags(ctx, table)
		owned := false
		for _, tag := range tags {
			if aws.ToString(tag.Key) == infraSetTagName && aws.ToString(tag.Value) == set {
				owned = true
			}
		}
		if err != nil || !owned {
			t.Fatalf("DynamoDB fixture ownership for %s: tags=%v err=%v", table, tags, err)
		}
	}
	configuration, err := LambdaClient().GetFunction(ctx, &lambda.GetFunctionInput{FunctionName: aws.String(function)})
	if err != nil || configuration == nil || configuration.Tags[infraSetTagName] != set {
		t.Fatalf("Lambda fixture is not owned or could not be read: %v", err)
	}
	streamARN, err := DynamoDBStreamArn(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, operation, userType, userValue, wantError string
	}{
		{"read", "GetItem", "N", "1", "ValidationException"},
		{"missing", "GetItem", "S", "missing-" + uid, "source item no longer exists"},
		{"write", "PutItem", "S", "jane", "ResourceNotFoundException"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.operation == "PutItem" {
				if err := DynamoDBDeleteTable(ctx, target, false, false); err != nil {
					t.Fatal(err)
				}
			}
			eventID, private := test.name+"-"+uid, "do-not-log-event-"+uid
			payload := Json(map[string]any{"Records": []any{map[string]any{
				"eventID": eventID, "eventSourceARN": streamARN, "private": private,
				"dynamodb": map[string]any{"Keys": map[string]any{
					"userid": map[string]string{test.userType: test.userValue}, "version": map[string]string{"N": "1"},
				}},
			}}})
			command := exec.CommandContext(ctx, "libaws", "lambda-invoke", function, "--payload-string", payload)
			var stdout, stderr strings.Builder
			command.Stdout, command.Stderr = &stdout, &stderr
			err := command.Run()
			if exit, ok := errors.AsType[*exec.ExitError](err); !ok || exit.ExitCode() != 1 || stdout.Len() != 0 {
				t.Fatalf("Lambda did not report %s failure: err=%v stdout=%s stderr=%s", test.operation, err, stdout.String(), stderr.String())
			}
			lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
			failure := lines[len(lines)-1]
			if !json.Valid([]byte(failure)) || !strings.Contains(failure, test.wantError) {
				t.Fatalf("unexpected Lambda failure payload: %s", failure)
			}
			logs := strings.Join(lines[:len(lines)-1], "\n")
			if strings.Contains(logs, private) {
				t.Fatal("failure logs exposed the event payload")
			}
			found := false
			for line := range strings.SplitSeq(logs, "\n") {
				start := strings.IndexByte(line, '{')
				if start < 0 {
					continue
				}
				var entry map[string]any
				if json.Unmarshal([]byte(line[start:]), &entry) == nil && entry["event_id"] == eventID &&
					entry["source_arn"] == streamARN && entry["operation"] == test.operation && entry["error"] != nil && entry["error"] != "" {
					found = true
				}
			}
			if !found {
				t.Fatalf("failure lacks record identity, operation, and error in structured logs: %s", logs)
			}
		})
		if t.Failed() {
			return
		}
	}
	if bucket := os.Getenv("LIBAWS_DYNAMODB_FAILURE_BUCKET"); bucket != "" {
		if bucket != "test-failed-events-"+uid {
			t.Fatal("failure bucket does not belong to this fixture")
		}
		t.Run("discarded stream invocation retains its complete payload", func(t *testing.T) {
			assertDynamoDBFailurePayload(t, ctx, source, bucket, uid)
		})
	}
}

func assertDynamoDBFailurePayload(t *testing.T, ctx context.Context, source, bucket, uid string) {
	t.Helper()
	// The output table was removed above. This real stream event must fail all
	// configured retries before Lambda itself delivers it to the S3 destination.
	marker := "retained-" + uid
	private := "private-failed-image-" + uid
	_, err := DynamoDBClient().PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(source),
		Item: map[string]ddbtypes.AttributeValue{
			"userid":  &ddbtypes.AttributeValueMemberS{Value: marker},
			"version": &ddbtypes.AttributeValueMemberN{Value: "1"},
			"data":    &ddbtypes.AttributeValueMemberS{Value: private},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		objects, err := S3Client().ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(bucket)})
		if err != nil {
			t.Fatal(err)
		}
		if aws.ToBool(objects.IsTruncated) {
			t.Fatal("unexpectedly large disposable failure bucket")
		}
		for _, object := range objects.Contents {
			response, err := S3Client().GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: object.Key})
			if err != nil {
				t.Fatal(err)
			}
			body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
			closeErr := response.Body.Close()
			if readErr != nil || closeErr != nil {
				t.Fatal("read failure destination", readErr, closeErr)
			}
			if response.ServerSideEncryption == "" {
				t.Fatal("failure payload lacks encryption at rest")
			}
			var envelope struct {
				RequestContext struct {
					Condition string `json:"condition"`
				} `json:"requestContext"`
				Payload string `json:"payload"`
			}
			if json.Unmarshal(body, &envelope) != nil {
				t.Fatal("invalid failure envelope")
			}
			var event events.DynamoDBEvent
			if json.Unmarshal([]byte(envelope.Payload), &event) != nil {
				t.Fatal("missing complete failure payload")
			}
			for _, record := range event.Records {
				if record.Change.Keys["userid"].String() != marker {
					continue
				}
				if record.Change.NewImage["data"].String() != private || envelope.RequestContext.Condition != "RetryAttemptsExhausted" {
					t.Fatal("discarded stream image or failure reason was lost")
				}
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("Lambda did not retain the discarded stream event")
		}
		time.Sleep(time.Second)
	}
}
