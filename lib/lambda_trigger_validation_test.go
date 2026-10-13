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
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
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

func TestDynamoDBFailureDestinationPreflight(t *testing.T) {
	for _, destination := range []string{"", "bucket", "arn:aws:sqs:us-east-1:123456789012:failures", "arn:aws:s3:::bucket/prefix"} {
		t.Run(destination, func(t *testing.T) {
			attrs := []string{"table", "start=trim_horizon", "on-failure=" + destination}
			file := filepath.Join(t.TempDir(), "infra.yaml")
			data := "name: fixture\nlambda:\n  function:\n    entrypoint: main.go\n    trigger:\n      - type: dynamodb\n        attr: " + Json(attrs) + "\n"
			if err := os.WriteFile(file, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := InfraParse(file); err == nil || !strings.Contains(err.Error(), "failure destination") {
				t.Errorf("invalid destination passed declaration parsing: %v", err)
			}
			for _, preview := range []bool{false, true} {
				for _, owner := range []string{"infra", "lambdas", "function", "mapping"} {
					t.Run(fmt.Sprintf("%s/preview=%t", owner, preview), func(t *testing.T) {
						ctx, cancel := context.WithCancel(t.Context())
						defer cancel()
						transport := &triggerValidationTransport{cancel: cancel}
						config := aws.Config{Region: "us-east-1", Credentials: aws.AnonymousCredentials{}, RetryMaxAttempts: 1, HTTPClient: transport}
						oldSession, oldS3, oldLambda, oldLogs, oldDDB := sess, s3Client, lambdaClient, logsClient, dynamoDBClient
						oldSTS, oldAccount := stsClient, stsAccount
						t.Cleanup(func() { stsClient, stsAccount = oldSTS, oldAccount })
						stsClient, stsAccount = sts.NewFromConfig(config), aws.String("123456789012")
						t.Cleanup(func() {
							sess, s3Client, lambdaClient, logsClient, dynamoDBClient = oldSession, oldS3, oldLambda, oldLogs, oldDDB
						})
						sess = &config
						s3Client, lambdaClient, logsClient, dynamoDBClient = s3.NewFromConfig(config), lambda.NewFromConfig(config), cloudwatchlogs.NewFromConfig(config), dynamodb.NewFromConfig(config)
						fn := &InfraLambda{Name: "function", Entrypoint: "main.go", Trigger: []*InfraTrigger{
							{Type: lambdaTriggerDynamoDB, Attr: []string{"valid-table", "start=latest"}},
							{Type: lambdaTriggerDynamoDB, Attr: attrs},
						}}
						set := &InfraSet{Name: "fixture", S3: map[string]*InfraS3{"bucket": {}}, Lambda: map[string]*InfraLambda{"function": fn}}
						var err error
						switch owner {
						case "infra":
							err = InfraEnsure(ctx, set, "", preview, false)
						case "lambdas":
							err = InfraEnsureLambda(ctx, set, "", preview, false)
						case "function":
							_, err = lambdaEnsureResources(ctx, fn, false, preview, false, nil, nil)
						case "mapping":
							err = LambdaEnsureTriggerDynamoDB(ctx, fn, preview)
						}
						if err == nil || !strings.Contains(err.Error(), "failure destination") || transport.calls != 0 {
							t.Fatalf("invalid destination reached provider: calls=%d err=%v", transport.calls, err)
						}
					})
				}
			}
		})
	}
}

func TestLambdaRetentionAlarmPreflight(t *testing.T) {
	for _, metric := range []string{"lambda-retained-failures", "lambda-destination-delivery-failures", "lambda-iterator-age"} {
		for _, preview := range []bool{false, true} {
			ctx, cancel := context.WithCancel(t.Context())
			transport := &triggerValidationTransport{cancel: cancel}
			config := aws.Config{Region: "us-east-1", Credentials: aws.AnonymousCredentials{}, RetryMaxAttempts: 1, HTTPClient: transport}
			oldS3, oldLogs := s3Client, logsClient
			s3Client, logsClient = s3.NewFromConfig(config), cloudwatchlogs.NewFromConfig(config)
			fn := &InfraLambda{Name: "function", Entrypoint: "main.go", Trigger: []*InfraTrigger{{Type: "alarm", Attr: []string{"name=alarm", metric + "=function", "dynamodb-table=table", "at-least=1/minute"}}}}
			// EventCount alone cannot emit retained events without a destination.
			fn.Trigger = append(fn.Trigger, &InfraTrigger{Type: "dynamodb", Attr: []string{"table", "start=latest", "metrics=event_count"}})
			set := &InfraSet{Name: "fixture", S3: map[string]*InfraS3{"bucket": {}}, Lambda: map[string]*InfraLambda{"function": fn}}
			for _, owner := range []func() error{
				func() error { return InfraEnsure(ctx, set, "", preview, false) },
				func() error { return InfraEnsureLambda(ctx, set, "", preview, false) },
				func() error { _, err := lambdaEnsureResources(ctx, fn, false, preview, false, nil, nil); return err },
			} {
				if err := owner(); err == nil {
					t.Errorf("invalid %s alarm accepted", metric)
				}
			}
			s3Client, logsClient = oldS3, oldLogs
			cancel()
			if transport.calls != 0 {
				t.Errorf("%s: mutated before validating alarm: %d provider calls", metric, transport.calls)
			}
		}
	}
}
