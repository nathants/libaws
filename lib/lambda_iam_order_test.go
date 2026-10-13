package lib

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
)

type lambdaIAMOrderTransport func(*http.Request) (*http.Response, error)

func (transport lambdaIAMOrderTransport) Do(request *http.Request) (*http.Response, error) {
	return transport(request)
}

// Exercise the full ensure owner through actual AWS SDK requests, including
// both permission families, failed prerequisites, activation and later pruning.
func TestLambdaEnsureIAMBeforeActivation(t *testing.T) {
	for _, creating := range []bool{false, true} {
		for _, mode := range []string{"activation", "inline-failure", "managed-failure", "api-failure", "api", "websocket", "prune", "destination", "destination-failure"} {
			if creating && strings.HasPrefix(mode, "destination") {
				continue
			}
			t.Run(fmt.Sprintf("create=%t/%s", creating, mode), func(t *testing.T) {
				t.Setenv(lambdaEnvVarApiID, "stale-http")
				t.Setenv(lambdaEnvVarWebsocketID, "stale-websocket")
				oldSession, oldIAM, oldLogs, oldLambda, oldAPI := sess, iamClient, logsClient, lambdaClient, apiClient
				oldAccount, oldARN := stsAccount, stsArn
				oldDDB := dynamoDBClient
				t.Cleanup(func() { dynamoDBClient = oldDDB })
				t.Cleanup(func() {
					sess, iamClient, logsClient, lambdaClient, apiClient = oldSession, oldIAM, oldLogs, oldLambda, oldAPI
					stsAccount, stsArn = oldAccount, oldARN
				})
				stsAccount = aws.String("123456789012")
				stsArn = aws.String("arn:aws:iam::123456789012:user/test")
				ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
				defer cancel()
				image := "123456789012.dkr.ecr.us-west-2.amazonaws.com/fixture@sha256:" + strings.Repeat("a", 64)
				oldAllow := &IamAllow{Action: "dynamodb:Scan", Resource: "arn:aws:dynamodb:us-west-2:123456789012:table/old"}
				newAllow := &IamAllow{Action: "dynamodb:Scan", Resource: "arn:aws:dynamodb:us-west-2:123456789012:table/new"}
				declaration := &InfraLambda{Name: "function", infraSetName: "fixture", runtime: lambdaRuntimeContainer, Attr: []string{"timeout=30"}, Entrypoint: image, Policy: []string{"new-managed"}}
				if mode == "api" || mode == "api-failure" || mode == "websocket" {
					kind, key := lambdaTriggerApi, lambdaEnvVarApiID
					if mode == "websocket" {
						kind, key = lambdaTriggerWebsocket, lambdaEnvVarWebsocketID
					}
					declaration.Trigger = []*InfraTrigger{{Type: kind}}
					newAllow = &IamAllow{Action: "execute-api:ManageConnections", Resource: "arn:aws:execute-api:us-west-2:123456789012:actual-api/*"}
					declaration.Allow = []string{strings.ReplaceAll(newAllow.String(), "actual-api", "${"+key+"}")}
				} else {
					declaration.Allow = []string{newAllow.String()}
				}
				destinationMode := strings.HasPrefix(mode, "destination")
				if destinationMode {
					oldAllow = &IamAllow{Action: "s3:PutObject", Resource: "arn:aws:s3:::old-failures/*"}
					newAllow = &IamAllow{Action: "s3:PutObject", Resource: "arn:aws:s3:::new-failures/*"}
					declaration.Allow = []string{newAllow.String()}
					declaration.Trigger = []*InfraTrigger{{Type: lambdaTriggerDynamoDB, Attr: []string{"table", "start=trim_horizon", "on-failure=arn:aws:s3:::new-failures"}}}
				}
				mappingUpdated, mappingSettled := false, false
				inline := map[string]string{oldAllow.policyName(): oldAllow.policyDocument()}
				managed := map[string]bool{"old-managed": true}
				activated, failureReached, pruned, apiCreated := false, false, false, false
				transport := lambdaIAMOrderTransport(func(request *http.Request) (*http.Response, error) {
					status, body := 200, `{}`
					if request.URL.Host == "iam.amazonaws.com" {
						data, err := io.ReadAll(request.Body)
						if err != nil {
							return nil, err
						}
						values, err := url.ParseQuery(string(data))
						if err != nil {
							return nil, err
						}
						action, result := values.Get("Action"), ""
						switch action {
						case "ListRoles":
							result = "<Roles></Roles>"
						case "CreateRole":
						case "ListPolicies":
							result = "<Policies><member><PolicyName>new-managed</PolicyName><Arn>arn:aws:iam::123456789012:policy/new-managed</Arn></member></Policies>"
						case "ListAttachedRolePolicies":
							result = "<AttachedPolicies>"
							for name := range managed {
								result += "<member><PolicyName>" + name + "</PolicyName><PolicyArn>arn:aws:iam::123456789012:policy/" + name + "</PolicyArn></member>"
							}
							result += "</AttachedPolicies>"
						case "AttachRolePolicy":
							if mode == "managed-failure" {
								failureReached = true
								cancel()
								return nil, context.Canceled
							}
							managed["new-managed"] = true
						case "DetachRolePolicy":
							if destinationMode && !mappingSettled {
								t.Error("old managed grant pruned before destination settled")
							}
							if !activated {
								t.Error("prior managed grant removed before activation")
							}
							delete(managed, "old-managed")
						case "GetRolePolicy":
							document, exists := inline[values.Get("PolicyName")]
							if !exists {
								status = 404
							} else {
								result = "<PolicyDocument>" + url.QueryEscape(document) + "</PolicyDocument>"
							}
						case "PutRolePolicy":
							if mode == "inline-failure" {
								failureReached = true
								cancel()
								return nil, context.Canceled
							}
							if values.Get("PolicyName") != newAllow.policyName() || values.Get("PolicyDocument") != newAllow.policyDocument() {
								t.Error("grant did not bind its actual resolved identity")
							}
							inline[values.Get("PolicyName")] = values.Get("PolicyDocument")
						case "ListRolePolicies":
							result = "<PolicyNames>"
							var names []string
							for name := range inline {
								names = append(names, name)
							}
							sort.Strings(names)
							for _, name := range names {
								result += "<member>" + name + "</member>"
							}
							result += "</PolicyNames>"
						case "DeleteRolePolicy":
							if destinationMode && !mappingSettled {
								t.Error("old destination write grant pruned before destination settled")
							}
							if !activated {
								t.Error("prior inline grant removed before activation")
							}
							if managed["old-managed"] || values.Get("PolicyName") != oldAllow.policyName() {
								t.Error("incorrect obsolete grant cleanup")
							}
							pruned = true
							cancel()
							return nil, context.Canceled
						default:
							return nil, fmt.Errorf("unexpected IAM action %s", action)
						}
						body = "<" + action + "Response><" + action + "Result>" + result + "</" + action + "Result></" + action + "Response>"
						if status == 404 {
							body = "<ErrorResponse><Error><Code>NoSuchEntity</Code><Message>absent</Message></Error></ErrorResponse>"
						}
					} else if strings.HasPrefix(request.URL.Host, "logs.") {
						switch request.Header.Get("X-Amz-Target") {
						case "Logs_20140328.DescribeLogStreams":
							body = `{"logStreams":[]}`
						case "Logs_20140328.DescribeLogGroups":
							body = `{"logGroups":[{"logGroupName":"/aws/lambda/function","retentionInDays":7}]}`
						case "Logs_20140328.PutRetentionPolicy":
						default:
							return nil, fmt.Errorf("unexpected logs action %s", request.Header.Get("X-Amz-Target"))
						}
					} else if strings.HasPrefix(request.URL.Host, "apigateway.") {
						if mode == "api-failure" {
							failureReached = true
							cancel()
							return nil, context.Canceled
						}
						if request.URL.Path != "/v2/apis" {
							return nil, fmt.Errorf("unexpected API request %s", request.URL.Path)
						}
						if request.Method == "POST" {
							apiCreated = true
						}
						body = `{"items":[]}`
						if apiCreated {
							name, protocol := "function", "HTTP"
							if mode == "websocket" {
								name, protocol = name+LambdaWebsocketSuffix, "WEBSOCKET"
							}
							body = `{"items":[{"name":"` + name + `","apiId":"actual-api","protocolType":"` + protocol + `"}]}`
						}
					} else if strings.HasPrefix(request.URL.Host, "dynamodb.") {
						body = `{"Table":{"LatestStreamArn":"arn:aws:dynamodb:us-west-2:123456789012:table/table/stream/current","StreamSpecification":{"StreamEnabled":true}}}`
					} else if strings.HasPrefix(request.URL.Host, "lambda.") {
						switch {
						case strings.Contains(request.URL.Path, "/event-source-mappings"):
							if request.Method == "PUT" {
								if pruned || inline[oldAllow.policyName()] == "" || !managed["old-managed"] {
									t.Error("destination switch lost prior grants")
								}
								if mode == "destination-failure" {
									failureReached = true
									cancel()
									return nil, context.Canceled
								}
								mappingUpdated = true
							}
							destination := "arn:aws:s3:::old-failures"
							if mappingUpdated {
								destination = "arn:aws:s3:::new-failures"
								if request.Method == "GET" {
									mappingSettled = true
								}
							}
							body = `{"UUID":"mapping","EventSourceArn":"arn:aws:dynamodb:us-west-2:123456789012:table/table/stream/current","State":"Enabled","BatchSize":100,"MaximumRetryAttempts":-1,"MaximumBatchingWindowInSeconds":0,"ParallelizationFactor":1,"StartingPosition":"TRIM_HORIZON","DestinationConfig":{"OnFailure":{"Destination":"` + destination + `"}}}`
							if strings.HasSuffix(request.URL.Path, "/event-source-mappings") {
								if destinationMode {
									body = `{"EventSourceMappings":[` + body + `]}`
								} else {
									body = `{}`
								}
							}
						case request.Method == "POST" && strings.HasSuffix(request.URL.Path, "/functions"), request.Method == "PUT" && strings.HasSuffix(request.URL.Path, "/code"):
							activated = true
							if inline[newAllow.policyName()] != newAllow.policyDocument() || !managed["new-managed"] {
								t.Error("Lambda code activated before its required IAM grants")
							}
							if inline[oldAllow.policyName()] != oldAllow.policyDocument() || !managed["old-managed"] {
								t.Error("activation lost prior-code grants")
							}
							if mode != "prune" && !destinationMode {
								cancel()
								return nil, context.Canceled
							}
						case request.Method == "GET" && strings.HasSuffix(request.URL.Path, "/configuration"):
							body = `{"Environment":{"Variables":{}},"Timeout":30,"MemorySize":128}`
						case request.Method == "GET" && strings.HasSuffix(request.URL.Path, "/functions/function"):
							current := "old"
							if activated {
								current = image
							}
							body = `{"Configuration":{"FunctionArn":"arn:aws:lambda:us-west-2:123456789012:function:function","State":"Active","LastUpdateStatus":"Successful","PackageType":"Image"},"Code":{"ImageUri":"` + current + `","ResolvedImageUri":"` + current + `"}}`
							if creating && !activated {
								status, body = 404, `{"Type":"User","message":"absent"}`
							}
						default:
							return nil, fmt.Errorf("unexpected Lambda request %s %s", request.Method, request.URL.Path)
						}
					} else {
						return nil, fmt.Errorf("unexpected AWS endpoint %s", request.URL.Host)
					}
					header := http.Header{}
					if status == 404 && strings.HasPrefix(request.URL.Host, "lambda.") {
						header.Set("X-Amzn-Errortype", "ResourceNotFoundException")
					}
					return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
				})
				config := aws.Config{Region: "us-west-2", Credentials: aws.AnonymousCredentials{}, RetryMaxAttempts: 1, HTTPClient: transport}
				sess = &config
				dynamoDBClient = dynamodb.NewFromConfig(config)
				iamClient, logsClient, lambdaClient, apiClient = iam.NewFromConfig(config), cloudwatchlogs.NewFromConfig(config), lambda.NewFromConfig(config), apigatewayv2.NewFromConfig(config)
				_, err := lambdaEnsureResources(ctx, declaration, false, false, false, nil, nil)
				if mode == "destination-failure" {
					if err == nil || !failureReached || !activated || pruned || !managed["old-managed"] || inline[oldAllow.policyName()] == "" {
						t.Fatalf("failed destination switch lost grants: activated=%t pruned=%t err=%v", activated, pruned, err)
					}
				} else if strings.HasSuffix(mode, "-failure") {
					if err == nil || !failureReached || activated || pruned {
						t.Fatalf("failed prerequisite did not fence activation: activated=%t pruned=%t err=%v", activated, pruned, err)
					}
				} else if !activated {
					t.Fatalf("ensure did not reach activation: %v", err)
				}
				if (mode == "prune" || mode == "destination") && !pruned {
					t.Fatalf("ensure did not reach post-activation pruning: %v", err)
				}
			})
		}
	}
}

func TestLambdaEnsureIAMAdditiveCollision(t *testing.T) {
	for _, preview := range []bool{false, true} {
		t.Run(fmt.Sprint(preview), func(t *testing.T) {
			t.Setenv(lambdaEnvVarApiID, "")
			t.Setenv(lambdaEnvVarWebsocketID, "")
			old := iamClient
			t.Cleanup(func() { iamClient = old })
			current := (&IamAllow{Action: "s3:GetObject", Resource: "arn:aws:s3:::old/*"}).policyDocument()
			reads := 0
			iamClient = iam.NewFromConfig(aws.Config{Region: "us-west-2", Credentials: aws.AnonymousCredentials{}, RetryMaxAttempts: 1, HTTPClient: lambdaIAMOrderTransport(func(request *http.Request) (*http.Response, error) {
				data, err := io.ReadAll(request.Body)
				if err != nil {
					return nil, err
				}
				values, err := url.ParseQuery(string(data))
				if err != nil {
					return nil, err
				}
				if values.Get("Action") != "GetRolePolicy" {
					t.Fatalf("collision mutated IAM: %s", values.Get("Action"))
				}
				reads++
				body := "<GetRolePolicyResponse><GetRolePolicyResult><PolicyDocument>" + url.QueryEscape(current) + "</PolicyDocument></GetRolePolicyResult></GetRolePolicyResponse>"
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
			})})
			err := iamEnsureRoleAllows(t.Context(), "fixture", []string{"s3:GetObject arn:aws:s3:::new/*"}, preview, false)
			if err == nil || !strings.Contains(err.Error(), "cannot preserve") || reads != 1 {
				t.Fatalf("collision not rejected before mutation: reads=%d err=%v", reads, err)
			}
		})
	}
}
