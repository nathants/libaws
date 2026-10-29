package lib

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
)

// The API example owns the unique function, API and log group through infra.yaml.
func TestAPIAccessLogsIntegration(t *testing.T) {
	name := os.Getenv("LIBAWS_API_LOGS_TEST_FUNCTION")
	if name == "" {
		t.Skip("run through examples/simple/go/api")
	}
	requireLiveAWSAccount(t)
	if !strings.HasPrefix(name, "test-lambda-") || name == "test-lambda-" {
		t.Fatal("requires a unique example fixture")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	function, err := LambdaClient().GetFunctionConfiguration(ctx, &lambda.GetFunctionConfigurationInput{FunctionName: aws.String(name)})
	if err != nil {
		t.Fatal(err)
	}
	tags, err := LambdaClient().ListTags(ctx, &lambda.ListTagsInput{Resource: function.FunctionArn})
	if err != nil {
		t.Fatal(err)
	}
	setName := "test-infraset-" + strings.TrimPrefix(name, "test-lambda-")
	if tags.Tags[infraSetTagName] != setName {
		t.Fatal("function is not owned by the API example")
	}
	api, err := Api(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	integrations, err := lambdaAPIIntegrations(ctx, ApiClient(), api.ApiId)
	if err != nil {
		t.Fatal(err)
	}
	if api.Tags[infraSetTagName] != setName || len(integrations) != 1 || !lambdaAPIIntegrationMatches(&integrations[0], aws.ToString(function.FunctionArn)) {
		t.Fatal("API does not belong to the selected fixture")
	}
	functionARN := aws.ToString(function.FunctionArn)
	apiID := aws.ToString(api.ApiId)
	assertEnabled := func(want bool) {
		t.Helper()
		got, err := lambdaReadAPIAccessLogs(ctx, ApiClient(), apiID, functionARN)
		if err != nil || (got != nil) != want || (got != nil && aws.ToString(got.Format) != lambdaAPIAccessLogFormat) {
			t.Fatalf("logging settings=%+v enabled want=%v error=%v", got, want, err)
		}
	}
	assertEnabled(true)
	t.Cleanup(func() {
		restore, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := lambdaEnsureAPIAccessLogs(restore, ApiClient(), api, functionARN, aws.Bool(true), false); err != nil {
			t.Errorf("restore access logs: %v", err)
		}
	})
	if err := lambdaEnsureAPIAccessLogs(ctx, ApiClient(), api, functionARN, aws.Bool(false), true); err != nil {
		t.Fatal(err)
	}
	assertEnabled(true)
	if err := lambdaEnsureAPIAccessLogs(ctx, ApiClient(), api, functionARN, aws.Bool(false), false); err != nil {
		t.Fatal(err)
	}
	assertEnabled(false)
	if err := lambdaEnsureAPIAccessLogs(ctx, ApiClient(), api, functionARN, aws.Bool(true), false); err != nil {
		t.Fatal(err)
	}
	assertEnabled(true)

	start := time.Now().Add(-time.Second).UnixMilli()
	wanted := map[string]int{}
	const privateMarker = "private-api-access-test-marker"
	client := &http.Client{Timeout: 35 * time.Second}
	for _, test := range []struct {
		path   string
		status int
	}{{"/", 200}, {"/backend-fail", 503}, {"/integration-fail", 502}} {
		url := fmt.Sprintf("https://%s.execute-api.%s.amazonaws.com%s?private=%s", apiID, Region(), test.path, privateMarker)
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(privateMarker))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+privateMarker)
		request.Header.Set("Cookie", "private="+privateMarker)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_, readErr := io.Copy(io.Discard, response.Body)
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("response body: read=%v close=%v", readErr, closeErr)
		}
		requestID := response.Header.Get("Apigw-Requestid")
		if response.StatusCode != test.status {
			t.Errorf("%s returned %d, want %d; request ID %s", test.path, response.StatusCode, test.status, requestID)
		}
		if requestID == "" {
			t.Fatal("response has no Gateway request ID")
		}
		wanted[requestID] = response.StatusCode
	}
	deadline := time.NewTimer(time.Minute)
	defer deadline.Stop()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		var output bytes.Buffer
		if err := LogsEvents(ctx, &output, "/aws/lambda/"+name, `"http-api-access"`, start, nil, aws.Int64(1000)); err != nil {
			t.Fatal(err)
		}
		decoder := json.NewDecoder(&output)
		for decoder.More() {
			var event logsEventRecord
			if err := decoder.Decode(&event); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(event.Message, privateMarker) {
				t.Fatal("access log leaked request content")
			}
			var record map[string]string
			if err := json.Unmarshal([]byte(event.Message), &record); err != nil {
				t.Fatalf("access log is not JSON: %v: %s", err, event.Message)
			}
			status, ok := wanted[record["request_id"]]
			if !ok {
				continue
			}
			t.Logf("correlated access record: %s", event.Message)
			if record["kind"] != "http-api-access" || record["api_id"] != apiID || record["status"] != fmt.Sprint(status) || record["response_ms"] == "" || record["response_ms"] == "-" || record["integration_request_id"] == "" || record["integration_request_id"] == "-" {
				t.Fatalf("missing response correlation: %s", event.Message)
			}
			if status == 503 && (record["integration_status"] != "200" || record["backend_status"] != "503") {
				t.Fatalf("backend error confused with provider failure: %s", event.Message)
			}
			if status == 502 && (record["integration_error"] == "" || record["integration_error"] == "-") {
				t.Fatalf("integration failure has no cause: %s", event.Message)
			}
			delete(wanted, record["request_id"])
		}
		if len(wanted) == 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-deadline.C:
			t.Fatalf("access log delivery missing request IDs: %v", wanted)
		case <-ticker.C:
		}
	}
}
