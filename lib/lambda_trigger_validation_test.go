package lib

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type triggerValidationTransport struct {
	calls  int
	cancel context.CancelFunc
}

func (transport *triggerValidationTransport) Do(*http.Request) (*http.Response, error) {
	transport.calls++
	transport.cancel()
	return nil, errors.New("unexpected provider request before trigger validation")
}

func TestInfraParseRequiresTriggerSource(t *testing.T) {
	for _, kind := range []string{lambdaTrigerS3, lambdaTriggerSQS} {
		for _, attrs := range [][]string{nil, {}, {""}, {" "}, {"batch=1"}, {"source"}, {"source", "batch=1"}} {
			t.Run(kind+"/"+Json(attrs), func(t *testing.T) {
				file := filepath.Join(t.TempDir(), "infra.yaml")
				data := "name: test-triggers\nlambda:\n  function:\n    entrypoint: main.go\n    trigger:\n      - type: " + kind + "\n"
				if attrs != nil {
					data += "        attr: " + Json(attrs) + "\n"
				}
				if err := os.WriteFile(file, []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
				_, err := InfraParse(file)
				valid := len(attrs) > 0 && attrs[0] == "source" && (kind == lambdaTriggerSQS || len(attrs) == 1)
				if valid {
					if err != nil {
						t.Fatalf("valid source rejected: %v", err)
					}
				} else if err == nil || !strings.Contains(err.Error(), kind+" trigger requires") {
					t.Fatalf("malformed trigger must fail locally: %v", err)
				}
			})
		}
	}
}

func TestLambdaEnsureRejectsMissingTriggerSourceBeforeProviderCalls(t *testing.T) {
	for _, kind := range []string{lambdaTrigerS3, lambdaTriggerSQS} {
		for _, preview := range []bool{false, true} {
			for _, mixed := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/preview=%v/mixed=%v", kind, preview, mixed), func(t *testing.T) {
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					transport := &triggerValidationTransport{cancel: cancel}
					config := aws.Config{Region: "us-east-1", Credentials: aws.AnonymousCredentials{}, RetryMaxAttempts: 1, HTTPClient: transport}
					oldLambda, oldS3, oldSession, oldAccount := lambdaClient, s3Client, sess, stsAccount
					t.Cleanup(func() { lambdaClient, s3Client, sess, stsAccount = oldLambda, oldS3, oldSession, oldAccount })
					sess, stsAccount = &config, aws.String("123456789012")
					lambdaClient, s3Client = lambda.NewFromConfig(config), s3.NewFromConfig(config)
					function := &InfraLambda{Name: "function", Arn: "arn:aws:lambda:us-east-1:123456789012:function:function"}
					if mixed {
						function.Trigger = append(function.Trigger, &InfraTrigger{Type: kind, Attr: []string{"source"}})
					}
					function.Trigger = append(function.Trigger, &InfraTrigger{Type: kind})
					defer func() {
						if value := recover(); value != nil {
							t.Errorf("missing %s trigger source panicked: %v", kind, value)
						}
						if transport.calls != 0 {
							t.Errorf("made %d provider requests before validating all %s sources", transport.calls, kind)
						}
					}()
					var err error
					if kind == lambdaTrigerS3 {
						_, err = LambdaEnsureTriggerS3(ctx, function, preview)
					} else {
						err = LambdaEnsureTriggerSQS(ctx, function, preview)
					}
					if err == nil || !strings.Contains(err.Error(), kind+" trigger requires") {
						t.Fatalf("expected useful source validation error: %v", err)
					}
				})
			}
		}
	}
}
