package lib

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/gofrs/uuid"
)

type dynamoDBTTLTransport func(*http.Request) (*http.Response, error)

func (transport dynamoDBTTLTransport) Do(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestDynamoDBEnsureTTLOnCreate(t *testing.T) {
	for _, test := range []struct {
		name      string
		ttl       bool
		preview   bool
		failAt    string
		wantCalls []string
		wantLog   string
	}{
		{
			name: "apply", ttl: true,
			wantCalls: []string{"DescribeTable", "CreateTable", "DescribeTable", "UpdateTimeToLive"},
		},
		{
			name:      "without TTL",
			wantCalls: []string{"DescribeTable", "CreateTable"},
		},
		{
			name: "preview", ttl: true, preview: true,
			wantCalls: []string{"DescribeTable"}, wantLog: "preview: enable ttl attr: expires",
		},
		{
			name: "create failure", ttl: true, failAt: "CreateTable",
			wantCalls: []string{"DescribeTable", "CreateTable"},
		},
		{
			name: "readiness failure", ttl: true, failAt: "DescribeTable",
			wantCalls: []string{"DescribeTable", "CreateTable", "DescribeTable"},
		},
		{
			name: "TTL update failure", ttl: true, failAt: "UpdateTimeToLive",
			wantCalls: []string{"DescribeTable", "CreateTable", "DescribeTable", "UpdateTimeToLive"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls []string
			var logs strings.Builder
			oldClient, oldLogger := dynamoDBClient, *Logger
			t.Cleanup(func() {
				dynamoDBClient, *Logger = oldClient, oldLogger
			})
			Logger.disabled = false
			Logger.Print = func(args ...any) { fmt.Fprint(&logs, args...) }
			transport := dynamoDBTTLTransport(func(request *http.Request) (*http.Response, error) {
				action := Last(strings.Split(request.Header.Get("X-Amz-Target"), "."))
				calls = append(calls, action)
				status, body := 200, `{}`
				switch {
				case len(calls) == 1 && action == "DescribeTable":
					status, body = 400, `{"__type":"ResourceNotFoundException","message":"table does not exist"}`
				case action == test.failAt:
					status, body = 400, fmt.Sprintf(`{"__type":"ValidationException","message":"injected %s failure"}`, action)
				case action == "DescribeTable":
					body = `{"Table":{"TableName":"test-ttl","TableStatus":"ACTIVE"}}`
				case action == "CreateTable":
					body = `{"TableDescription":{"TableName":"test-ttl","TableStatus":"CREATING"}}`
				case action == "UpdateTimeToLive":
					var input dynamodb.UpdateTimeToLiveInput
					if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
						return nil, err
					}
					if aws.ToString(input.TableName) != "test-ttl" || input.TimeToLiveSpecification == nil ||
						aws.ToString(input.TimeToLiveSpecification.AttributeName) != "expires" || !aws.ToBool(input.TimeToLiveSpecification.Enabled) {
						t.Errorf("unexpected TTL request: %s", Json(input))
					}
				default:
					return nil, fmt.Errorf("unexpected request: %s", action)
				}
				return &http.Response{
					StatusCode: status, Header: http.Header{"Content-Type": {"application/x-amz-json-1.0"}},
					Body: io.NopCloser(strings.NewReader(body)), Request: request,
				}, nil
			})
			dynamoDBClient = dynamodb.NewFromConfig(aws.Config{
				Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
				HTTPClient: transport, RetryMaxAttempts: 1,
			})
			var attrs []string
			if test.ttl {
				attrs = []string{"ttl=expires"}
			}
			input, ttl, err := DynamoDBEnsureInput("test-set", "test-ttl", []string{"id:s:hash"}, attrs)
			if err != nil {
				t.Fatal(err)
			}
			err = DynamoDBEnsure(context.Background(), input, ttl, test.preview)
			if test.failAt == "" && err != nil {
				t.Fatal(err)
			}
			if test.failAt != "" && (err == nil || !strings.Contains(err.Error(), "injected "+test.failAt+" failure")) {
				t.Fatalf("expected %s error, got %v", test.failAt, err)
			}
			if !slices.Equal(calls, test.wantCalls) {
				t.Errorf("calls = %v, want %v", calls, test.wantCalls)
			}
			if test.wantLog != "" && !strings.Contains(logs.String(), test.wantLog) {
				t.Errorf("preview did not report TTL configuration: %s", logs.String())
			}
		})
	}
}

func TestDynamoDBTTLCreateIntegration(t *testing.T) {
	requireLiveAWSAccount(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	name := "test-ttl-" + uuid.Must(uuid.NewV4()).String()
	input, ttl, err := DynamoDBEnsureInput(name, name, []string{"id:s:hash"}, []string{"ttl=expires"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cleanupCancel()
		if err := DynamoDBDeleteTable(cleanupCtx, name, false, false); err != nil {
			t.Errorf("delete TTL fixture: %v", err)
		}
	})
	if err := DynamoDBEnsure(ctx, input, ttl, false); err != nil {
		t.Fatal(err)
	}
	// Inspect AWS before any re-ensure can repair an omitted TTL update.
	out, err := dynamoDBWaitForTTL(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	if out.TimeToLiveDescription.TimeToLiveStatus != ddbtypes.TimeToLiveStatusEnabled ||
		aws.ToString(out.TimeToLiveDescription.AttributeName) != "expires" {
		t.Fatalf("TTL after first ensure: %s", Json(out.TimeToLiveDescription))
	}
	if err := DynamoDBEnsure(ctx, input, ttl, false); err != nil {
		t.Fatalf("re-ensure TTL table: %v", err)
	}
}
