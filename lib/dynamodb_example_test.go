package lib

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
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
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
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
	bucket := os.Getenv("LIBAWS_DYNAMODB_FAILURE_BUCKET")
	if bucket != "" {
		if bucket != "test-failed-events-"+uid {
			t.Fatal("failure bucket does not belong to this fixture")
		}
		if !t.Run("destination transition retains IAM grants", func(t *testing.T) {
			assertDynamoDBDestinationIAM(t, ctx, function, bucket, uid)
		}) {
			return
		}
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
	if bucket != "" {
		for _, view := range []ddbtypes.StreamViewType{
			ddbtypes.StreamViewTypeKeysOnly, ddbtypes.StreamViewTypeNewImage,
			ddbtypes.StreamViewTypeOldImage, ddbtypes.StreamViewTypeNewAndOldImages,
		} {
			if !t.Run("retained payload/"+string(view), func(t *testing.T) {
				// Stream type is immutable while enabled. Rotate the owned
				// stream, then reconcile its destination through the real owner.
				for _, spec := range []*ddbtypes.StreamSpecification{
					{StreamEnabled: aws.Bool(false)},
					{StreamEnabled: aws.Bool(true), StreamViewType: view},
				} {
					if _, err := DynamoDBClient().UpdateTable(ctx, &dynamodb.UpdateTableInput{TableName: aws.String(source), StreamSpecification: spec}); err != nil {
						t.Fatal(err)
					}
					if err := DynamoDBWaitForReady(ctx, source); err != nil {
						t.Fatal(err)
					}
				}
				declaration := &InfraLambda{Name: function, Trigger: []*InfraTrigger{{Type: lambdaTriggerDynamoDB,
					Attr: []string{source, "start=trim_horizon", "batch=1", "retry=3", "on-failure=arn:aws:s3:::" + bucket},
				}}}
				if err := LambdaEnsureTriggerDynamoDB(ctx, declaration, false); err != nil {
					t.Fatal(err)
				}
				assertDynamoDBFailurePayload(t, ctx, source, bucket, uid, view)
			}) {
				return
			}
		}
	}
}

func assertDynamoDBFailurePayload(t *testing.T, ctx context.Context, source, bucket, uid string, view ddbtypes.StreamViewType) {
	t.Helper()
	// The output table was removed above. This real stream event must fail all
	// configured retries before Lambda itself delivers it to the S3 destination.
	marker := "retained-" + uid + "-" + string(view)
	// A MODIFY event provides both an old and a new image where the selected
	// stream view includes them. Keys-only must not acquire invented images.
	for _, data := range []string{"old-private-" + uid, "new-private-" + uid} {
		_, err := DynamoDBClient().PutItem(ctx, &dynamodb.PutItemInput{
			TableName: aws.String(source),
			Item: map[string]ddbtypes.AttributeValue{
				"userid":  &ddbtypes.AttributeValueMemberS{Value: marker},
				"version": &ddbtypes.AttributeValueMemberN{Value: "1"},
				"data":    &ddbtypes.AttributeValueMemberS{Value: data},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
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
				if record.Change.Keys["userid"].String() != marker || record.EventName != "MODIFY" {
					continue
				}
				if envelope.RequestContext.Condition != "RetryAttemptsExhausted" || record.Change.StreamViewType != string(view) || len(record.Change.Keys) != 2 || record.Change.Keys["version"].Number() != "1" {
					t.Fatal("discarded stream keys, view, or failure reason was lost")
				}
				for _, image := range []struct {
					value map[string]events.DynamoDBAttributeValue
					want  bool
					data  string
				}{
					{record.Change.OldImage, view == ddbtypes.StreamViewTypeOldImage || view == ddbtypes.StreamViewTypeNewAndOldImages, "old-private-" + uid},
					{record.Change.NewImage, view == ddbtypes.StreamViewTypeNewImage || view == ddbtypes.StreamViewTypeNewAndOldImages, "new-private-" + uid},
				} {
					if !image.want {
						if len(image.value) != 0 {
							t.Fatal("destination added an image absent from the stream view")
						}
					} else if len(image.value) != 3 || image.value["userid"].String() != marker || image.value["version"].Number() != "1" || image.value["data"].String() != image.data {
						t.Fatal("destination lost or changed a stream image")
					}
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

type dynamoDBDestinationTransport func(*http.Request) (*http.Response, error)

func (transport dynamoDBDestinationTransport) Do(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func assertDynamoDBDestinationIAM(t *testing.T, ctx context.Context, function, bucket, uid string) {
	t.Helper()
	t.Setenv("uid", uid)
	original, err := InfraParse("../examples/simple/go/dynamodb/infra.yaml")
	if err != nil {
		t.Fatal(err)
	}
	desired, err := InfraParse("../examples/simple/go/dynamodb/infra.yaml")
	if err != nil {
		t.Fatal(err)
	}
	fn := desired.Lambda[function]
	fn.Allow = slices.DeleteFunc(fn.Allow, func(grant string) bool { return strings.Contains(grant, "arn:aws:s3:::"+bucket) })
	fn.Trigger[0].Attr = slices.DeleteFunc(fn.Trigger[0].Attr, func(attr string) bool { return strings.HasPrefix(attr, "on-failure=") })
	grants := []*IamAllow{
		{Action: "s3:PutObject", Resource: "arn:aws:s3:::" + bucket + "/*"},
		{Action: "s3:ListBucket", Resource: "arn:aws:s3:::" + bucket},
	}
	checkGrants := func(present bool) error {
		for _, grant := range grants {
			_, err := IamClient().GetRolePolicy(ctx, &iam.GetRolePolicyInput{RoleName: aws.String(function), PolicyName: aws.String(grant.policyName())})
			if present && err != nil {
				return err
			}
			if !present {
				if err == nil {
					return errors.New("obsolete grant remains")
				}
				if _, ok := errors.AsType[*iamtypes.NoSuchEntityException](err); !ok {
					return fmt.Errorf("read obsolete grant: %w", err)
				}
			}
		}
		return nil
	}
	oldClient := LambdaClient()
	defer func() { lambdaClient = oldClient }()
	base := Session().HTTPClient
	if base == nil {
		base = http.DefaultClient
	}
	for _, fail := range []bool{true, false} {
		attempt, cancel := context.WithCancel(ctx)
		reached := false
		lambdaClient = lambda.NewFromConfig(*Session(), func(options *lambda.Options) {
			options.HTTPClient = dynamoDBDestinationTransport(func(request *http.Request) (*http.Response, error) {
				if request.Method == "PUT" && strings.Contains(request.URL.Path, "/event-source-mappings/") {
					reached = true
					// Read real IAM at the dispatch boundary, including the
					// attempt canceled after actual ensure-side IAM mutations.
					if err := checkGrants(true); err != nil {
						cancel()
						return nil, err
					}
					if fail {
						cancel()
						return nil, context.Canceled
					}
				}
				return base.Do(request)
			})
		})
		err := InfraEnsureLambda(attempt, desired, "", false, false)
		cancel()
		lambdaClient = oldClient
		if !reached || fail && !errors.Is(err, context.Canceled) || !fail && err != nil {
			t.Fatalf("destination transition fail=%t: reached=%t err=%v", fail, reached, err)
		}
		if err := checkGrants(fail); err != nil {
			t.Fatal(err)
		}
		mappings, err := lambdaListEventSourceMappings(ctx, function)
		if err != nil || len(mappings) != 1 {
			t.Fatalf("read mapping: %v %v", mappings, err)
		}
		want := ""
		if fail {
			want = "arn:aws:s3:::" + bucket
		}
		if dynamoDBFailureDestination(mappings[0].DestinationConfig) != want || aws.ToString(mappings[0].State) != "Enabled" {
			t.Fatal("destination transition left incorrect mapping state")
		}
	}
	if err := InfraEnsureLambda(ctx, original, "", false, false); err != nil {
		t.Fatal(err)
	}
	if err := checkGrants(true); err != nil {
		t.Fatal(err)
	}
}
