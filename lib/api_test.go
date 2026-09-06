package lib

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
)

type apiListTransport func(*http.Request) (*http.Response, error)

func (transport apiListTransport) Do(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestApiListRequiresCompleteIdentity(t *testing.T) {
	for _, test := range []struct {
		name, malformed  string
		persistent, deny bool
	}{
		{name: "missing name", malformed: `{"apiId":"second"}`},
		{name: "missing ID", malformed: `{"name":"second-name"}`},
		{name: "persistent incomplete page", malformed: `{}`, persistent: true},
		{name: "provider error", deny: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			old := apiClient
			t.Cleanup(func() { apiClient = old })
			reads := 0
			apiClient = apigatewayv2.NewFromConfig(aws.Config{
				Region: "us-east-1", Credentials: aws.AnonymousCredentials{}, RetryMaxAttempts: 1,
				HTTPClient: apiListTransport(func(request *http.Request) (*http.Response, error) {
					reads++
					status, body := 200, `{"items":[{"apiId":"first","name":"first-name"}],"nextToken":"next"}`
					if reads > 1 {
						if request.URL.Query().Get("nextToken") != "next" {
							return nil, fmt.Errorf("pagination token changed during page retry: %s", request.URL)
						}
						body = `{"items":[{"apiId":"second","name":"second-name"}]}`
						if reads == 2 || test.persistent {
							body = `{"items":[` + test.malformed + `]}`
						}
					}
					if test.deny {
						status, body = 403, `{"message":"injected provider denial"}`
					}
					return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
				}),
			})
			apis, err := ApiList(context.Background())
			if test.deny {
				if err == nil || !strings.Contains(err.Error(), "injected provider denial") || reads != 1 || len(apis) != 0 {
					t.Fatalf("provider error was lost or outer-retried: reads=%d apis=%v err=%v", reads, apis, err)
				}
			} else if test.persistent {
				if err == nil || !strings.Contains(err.Error(), "incomplete API metadata") || reads != 7 || len(apis) != 0 {
					t.Fatalf("incomplete inventory accepted or retry unbounded: reads=%d apis=%v err=%v", reads, apis, err)
				}
			} else if err != nil || reads != 3 || len(apis) != 2 || aws.ToString(apis[0].ApiId) != "first" || aws.ToString(apis[1].ApiId) != "second" || aws.ToString(apis[1].Name) != "second-name" {
				t.Fatalf("incomplete API identity escaped to callers: reads=%d apis=%v err=%v", reads, apis, err)
			}
		})
	}
}
