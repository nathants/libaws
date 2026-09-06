package lib

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cwlogstypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/gofrs/uuid"
)

type logsRetentionTransport func(*http.Request) (*http.Response, error)

func (transport logsRetentionTransport) Do(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestLogsEnsureRetention(t *testing.T) {
	for _, test := range []struct {
		name             string
		current, desired int
		preview, fail    bool
		mutation         string
	}{
		{name: "remove", current: 7, mutation: "DeleteRetentionPolicy"},
		{name: "remove failure", current: 7, fail: true, mutation: "DeleteRetentionPolicy"},
		{name: "preview removal", current: 7, preview: true},
		{name: "already unlimited"},
		{name: "enable", desired: 7, mutation: "PutRetentionPolicy"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var mutations []string
			old := logsClient
			t.Cleanup(func() { logsClient = old })
			logsClient = cloudwatchlogs.NewFromConfig(aws.Config{
				Region: "us-east-1", Credentials: aws.AnonymousCredentials{}, RetryMaxAttempts: 1,
				HTTPClient: logsRetentionTransport(func(request *http.Request) (*http.Response, error) {
					action := Last(strings.Split(request.Header.Get("X-Amz-Target"), "."))
					status, body := 200, `{}`
					switch action {
					case "DescribeLogStreams":
						body = `{"logStreams":[]}`
					case "DescribeLogGroups":
						group := map[string]any{"logGroupName": "fixture"}
						if test.current > 0 {
							group["retentionInDays"] = test.current
						}
						body = Json(map[string]any{"logGroups": []any{group}})
					case "PutRetentionPolicy", "DeleteRetentionPolicy":
						mutations = append(mutations, action)
						var input struct {
							LogGroupName    string
							RetentionInDays int
						}
						if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
							return nil, err
						}
						if input.LogGroupName != "fixture" || (action == "PutRetentionPolicy" && input.RetentionInDays != test.desired) {
							return nil, fmt.Errorf("unexpected retention input: %+v", input)
						}
						if test.fail {
							status, body = 400, `{"__type":"AccessDeniedException","message":"injected retention denial"}`
						}
					default:
						return nil, fmt.Errorf("unexpected request: %s", action)
					}
					return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
				}),
			})
			err := LogsEnsureGroup(context.Background(), "fixture", "fixture", test.desired, test.preview)
			if test.fail {
				if err == nil || !strings.Contains(err.Error(), "injected retention denial") {
					t.Fatalf("retention failure lost: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			var want []string
			if test.mutation != "" {
				want = []string{test.mutation}
			}
			if !slices.Equal(mutations, want) {
				t.Fatalf("retention requests=%v, want %v", mutations, want)
			}
		})
	}
}

func TestLogsRetentionIntegration(t *testing.T) {
	requireLiveAWSAccount(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	name := "/libaws/test-retention-" + uuid.Must(uuid.NewV4()).String()
	client := LogsClient()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		var absent *cwlogstypes.ResourceNotFoundException
		if err := LogsDeleteGroup(cleanupCtx, name, false); err != nil {
			t.Errorf("delete retention fixture: %v", err)
		}
		if _, err := client.DescribeLogStreams(cleanupCtx, &cloudwatchlogs.DescribeLogStreamsInput{LogGroupName: aws.String(name)}); !errors.As(err, &absent) {
			t.Errorf("retention fixture remains or deletion could not be verified: %v", err)
		}
	})
	for _, step := range []struct {
		days, want int
		preview    bool
	}{{7, 7, false}, {0, 7, true}, {0, 0, false}, {0, 0, false}, {7, 7, false}} {
		if err := LogsEnsureGroup(ctx, name, name, step.days, step.preview); err != nil {
			t.Fatal(err)
		}
		out, err := client.DescribeLogGroups(ctx, &cloudwatchlogs.DescribeLogGroupsInput{LogGroupNamePrefix: aws.String(name)})
		if err != nil || len(out.LogGroups) != 1 || int(aws.ToInt32(out.LogGroups[0].RetentionInDays)) != step.want {
			t.Fatalf("retention after days=%d preview=%v: out=%v err=%v", step.days, step.preview, out, err)
		}
	}
}
