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

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type infraSetTransport func(*http.Request) (*http.Response, error)

func (f infraSetTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func infraSetResponse(r *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

func installInfraSetClients(t *testing.T, transport http.RoundTripper) {
	t.Helper()
	config := aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("key", "secret", ""), HTTPClient: &http.Client{Transport: transport}, RetryMaxAttempts: 1}
	oldSession, oldAccount := sess, stsAccount
	oldDynamo, oldIAM, oldLambda, oldSQS, oldEvents := dynamoDBClient, iamClient, lambdaClient, sqsClient, eventsClient
	oldAPI, oldDNS := apiClient, r53Client
	sess, stsAccount = &config, aws.String("123456789012")
	dynamoDBClient, iamClient = dynamodb.NewFromConfig(config), iam.NewFromConfig(config)
	lambdaClient, sqsClient, eventsClient = lambda.NewFromConfig(config), sqs.NewFromConfig(config), eventbridge.NewFromConfig(config)
	apiClient, r53Client = apigatewayv2.NewFromConfig(config), route53.NewFromConfig(config)
	t.Cleanup(func() {
		sess, stsAccount = oldSession, oldAccount
		dynamoDBClient, iamClient, lambdaClient, sqsClient, eventsClient = oldDynamo, oldIAM, oldLambda, oldSQS, oldEvents
		apiClient, r53Client = oldAPI, oldDNS
	})
}

func TestInfraListSetMembershipBeforeDescription(t *testing.T) {
	for _, service := range []string{"dynamodb", "lambda", "sqs", "event", "user", "role", "profile"} {
		for _, tag := range []string{"wanted", "wanted-suffix", "", "other", "missing", "denied"} {
			t.Run(service+"/tag="+tag, func(t *testing.T) {
				installInfraSetClients(t, infraSetTransport(func(r *http.Request) (*http.Response, error) {
					body := ""
					target := Last(strings.Split(r.Header.Get("X-Amz-Target"), "."))
					switch target {
					default:
					case "ListTables":
						body = `{"TableNames":["cross-named-table"]}`
					case "ListTagsOfResource", "ListTagsForResource":
						body = fmt.Sprintf(`{"Tags":[{"Key":"libaws.infraset","Value":%q}]}`, tag)
					case "ListQueues":
						body = `{"QueueUrls":["https://sqs.us-east-1.amazonaws.com/123456789012/cross-named-queue"]}`
					case "ListQueueTags":
						body = fmt.Sprintf(`{"Tags":{"libaws.infraset":%q}}`, tag)
					case "ListRules":
						body = `{"Rules":[{"Name":"cross-named-rule","Arn":"arn:aws:events:us-east-1:123456789012:rule/cross-named-rule"}]}`
					}
					if strings.HasSuffix(r.URL.Path, "/functions") {
						body = `{"Functions":[{"FunctionName":"cross-named-function","FunctionArn":"arn:aws:lambda:us-east-1:123456789012:function:cross-named-function"}]}`
					} else if strings.Contains(r.URL.Path, "/tags/") {
						body = fmt.Sprintf(`{"Tags":{"libaws.infraset":%q}}`, tag)
					}
					if r.URL.Host == "iam.amazonaws.com" {
						if err := r.ParseForm(); err != nil {
							return nil, err
						}
						action := r.Form.Get("Action")
						switch action {
						default:
						case "ListUsers":
							body = `<Users><member><UserName>cross-named-user</UserName></member></Users>`
						case "ListRoles":
							body = `<Roles><member><RoleName>cross-named-role</RoleName><AssumeRolePolicyDocument>{}</AssumeRolePolicyDocument></member></Roles>`
						case "ListInstanceProfiles":
							body = `<InstanceProfiles><member><InstanceProfileName>cross-named-profile</InstanceProfileName><Roles><member><RoleName>cross-named-role</RoleName><AssumeRolePolicyDocument>{}</AssumeRolePolicyDocument></member></Roles></member></InstanceProfiles>`
						case "ListUserTags", "ListRoleTags", "ListInstanceProfileTags":
							body = `<Tags><member><Key>libaws.infraset</Key><Value>` + tag + `</Value></member></Tags>`
						}
						if body != "" {
							body = "<" + action + "Response><" + action + "Result>" + body + "</" + action + "Result></" + action + "Response>"
						}
					}
					membership := strings.Contains(target, "Tags") || strings.Contains(r.Form.Get("Action"), "Tags") || strings.Contains(r.URL.Path, "/tags/")
					if membership && (tag == "missing" || tag == "denied") {
						code, status := "ResourceNotFoundException", http.StatusNotFound
						if service == "sqs" {
							code = "QueueDoesNotExist"
						}
						if r.URL.Host == "iam.amazonaws.com" {
							code = "NoSuchEntity"
						}
						if tag == "denied" {
							code, status = "AccessDeniedException", http.StatusForbidden
						}
						body = fmt.Sprintf(`{"__type":%q,"message":"membership probe"}`, code)
						if r.URL.Host == "iam.amazonaws.com" {
							body = `<ErrorResponse><Error><Code>` + code + `</Code><Message>membership probe</Message></Error></ErrorResponse>`
						}
						response := infraSetResponse(r, status, body)
						response.Header.Set("X-Amzn-Errortype", code)
						return response, nil
					}
					if body == "" {
						return nil, fmt.Errorf("configuration probe: %s %s %s", target, r.URL, r.Form.Get("Action"))
					}
					return infraSetResponse(r, 200, body), nil
				}))
				scope := infraListScope{setName: "wanted"}
				var err error
				switch service {
				default:
					t.Fatalf("unexpected service: %s", service)
				case "dynamodb":
					_, err = scope.listDynamoDB(context.Background())
				case "lambda":
					ch := make(chan *InfraTrigger)
					close(ch)
					_, err = scope.listLambda(context.Background(), ch, "")
				case "sqs":
					_, err = scope.listSQS(context.Background())
				case "event":
					_, err = scope.listEvent(context.Background(), make(chan *InfraTrigger, 1))
				case "user":
					_, err = scope.listUser(context.Background())
				case "role":
					_, err = scope.listRole(context.Background())
				case "profile":
					_, err = scope.listInstanceProfile(context.Background())
				}
				if tag == "denied" {
					if err == nil || !strings.Contains(err.Error(), "membership probe") {
						t.Fatalf("membership error lost: %v", err)
					}
				} else if tag == "wanted" {
					if err == nil || !strings.Contains(err.Error(), "configuration probe") {
						t.Fatalf("selected configuration error lost: %v", err)
					}
				} else if err != nil {
					t.Fatalf("unrelated resource was described: %v", err)
				}
			})
		}
	}
}

func TestInfraListSetRequiresName(t *testing.T) {
	for _, name := range []string{"", " "} {
		if _, err := InfraListSet(context.Background(), name, false); err == nil {
			t.Fatal("accepted empty set name")
		}
	}
}

func TestInfraListDynamoDBRefreshesTTLStatus(t *testing.T) {
	for _, cancelDuringWait := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelDuringWait), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reads := 0
			installInfraSetClients(t, infraSetTransport(func(r *http.Request) (*http.Response, error) {
				var body string
				switch Last(strings.Split(r.Header.Get("X-Amz-Target"), ".")) {
				case "ListTables":
					body = `{"TableNames":["table"]}`
				case "ListTagsOfResource":
					body = `{"Tags":[{"Key":"libaws.infraset","Value":"wanted"}]}`
				case "DescribeTable":
					body = `{"Table":{"TableName":"table"}}`
				case "DescribeTimeToLive":
					reads++
					status := "ENABLED"
					if reads == 1 {
						status = "ENABLING"
						if cancelDuringWait {
							cancel()
						}
					}
					body = fmt.Sprintf(`{"TimeToLiveDescription":{"TimeToLiveStatus":%q,"AttributeName":"expires"}}`, status)
				default:
					return nil, fmt.Errorf("unexpected request: %s", r.Header.Get("X-Amz-Target"))
				}
				return infraSetResponse(r, 200, body), nil
			}))
			out, err := (infraListScope{setName: "wanted"}).listDynamoDB(ctx)
			if cancelDuringWait {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("canceled TTL wait: %v", err)
				}
			} else if err != nil || reads != 2 || !slices.Contains(out["table"].Attr, "ttl=expires") {
				t.Fatalf("TTL inventory: out=%v reads=%d error=%v", out, reads, err)
			}
		})
	}
}

func TestInfraListSetPreservesSelectedTableDisappearance(t *testing.T) {
	installInfraSetClients(t, infraSetTransport(func(r *http.Request) (*http.Response, error) {
		switch Last(strings.Split(r.Header.Get("X-Amz-Target"), ".")) {
		case "ListTables":
			return infraSetResponse(r, 200, `{"TableNames":["table"]}`), nil
		case "ListTagsOfResource":
			return infraSetResponse(r, 200, `{"Tags":[{"Key":"libaws.infraset","Value":"wanted"}]}`), nil
		case "DescribeTable":
			return infraSetResponse(r, 400, `{"__type":"ResourceNotFoundException","message":"selected table vanished"}`), nil
		default:
			return nil, fmt.Errorf("unexpected request: %s", r.Header.Get("X-Amz-Target"))
		}
	}))
	if _, err := (infraListScope{setName: "wanted"}).listDynamoDB(context.Background()); err == nil {
		t.Fatal("lost selected resource error")
	}
	// The existing global inventory still tolerates DescribeTable disappearance.
	if _, err := InfraListDynamoDB(context.Background()); err != nil {
		t.Fatalf("changed global disappearance handling: %v", err)
	}
}

func TestInfraListSetSESUsesExactInvocationGrants(t *testing.T) {
	const function = "arn:aws:lambda:us-east-1:123456789012:function:cross-named-function"
	const source = "arn:aws:ses:us-east-1:123456789012:receipt-rule-set/mail.example.com:receipt-rule/mail.example.com"
	for _, scenario := range []string{"exact", "wildcard", "wrong-account", "wrong-rule", "other-service", "no-policy", "denied", "invalid-json"} {
		t.Run(scenario, func(t *testing.T) {
			installInfraSetClients(t, infraSetTransport(func(r *http.Request) (*http.Response, error) {
				if !strings.HasSuffix(r.URL.Path, "/policy") {
					return nil, fmt.Errorf("unexpected request outside function policy: %s", r.URL)
				}
				if scenario == "no-policy" || scenario == "denied" {
					code, status := "ResourceNotFoundException", http.StatusNotFound
					if scenario == "denied" {
						code, status = "AccessDeniedException", http.StatusForbidden
					}
					response := infraSetResponse(r, status, fmt.Sprintf(`{"__type":%q,"message":"policy probe"}`, code))
					response.Header.Set("X-Amzn-Errortype", code)
					return response, nil
				}
				principal, arn := "ses.amazonaws.com", source
				switch scenario {
				case "wildcard":
					arn = strings.ReplaceAll(arn, "mail.example.com", "*")
				case "wrong-account":
					arn = strings.ReplaceAll(arn, "123456789012", "999999999999")
				case "wrong-rule":
					arn = strings.ReplaceAll(arn, ":receipt-rule/mail.example.com", ":receipt-rule/other.example.com")
				case "other-service":
					principal = "sns.amazonaws.com"
				default:
				}
				statement := fmt.Sprintf(`{"Effect":"Allow","Principal":{"Service":%q},"Action":"lambda:InvokeFunction","Resource":%q,"Condition":{"ArnLike":{"AWS:SourceArn":%q}}}`, principal, function, arn)
				policy := `{"Statement":[` + statement + `,` + statement + `]}` // Deduplicate candidate reads.
				if scenario == "invalid-json" {
					policy = "not json"
				}
				body, err := json.Marshal(map[string]string{"Policy": policy})
				if err != nil {
					return nil, err
				}
				return infraSetResponse(r, 200, string(body)), nil
			}))
			rules, err := (infraListScope{setName: "wanted"}).sesRules(context.Background(), function)
			switch scenario {
			case "exact":
				if err != nil || len(rules) != 1 || aws.ToString(rules[0].Name) != "mail.example.com" {
					t.Fatalf("exact rule candidate: rules=%v err=%v", rules, err)
				}
			case "other-service", "no-policy":
				if err != nil || len(rules) != 0 {
					t.Fatalf("unexpected SES candidates: rules=%v err=%v", rules, err)
				}
			default:
				if err == nil {
					t.Fatal("silently ignored unsupported or unreadable SES permission")
				}
			}
		})
	}
}

func TestInfraListSetAPIBoundsDomainAndDNSReads(t *testing.T) {
	for _, ownedDomain := range []bool{false, true} {
		t.Run(fmt.Sprint(ownedDomain), func(t *testing.T) {
			mappingReads, dnsReads := 0, 0
			installInfraSetClients(t, infraSetTransport(func(r *http.Request) (*http.Response, error) {
				var body string
				switch r.URL.Path {
				case "/v2/apis":
					body = `{"items":[{"apiId":"selected","name":"cross-named-function","tags":{"libaws.infraset":"wanted"}},{"apiId":"unrelated","name":"other-function","tags":{"libaws.infraset":"wanted-suffix"}}]}`
				case "/v2/domainnames":
					tag := "other"
					if ownedDomain {
						tag = "wanted"
					}
					body = fmt.Sprintf(`{"items":[{"domainName":"api.example.com","tags":{"libaws.infraset":%q,"libaws.route53-zone":"ZSELECTED"},"domainNameConfigurations":[{"apiGatewayDomainName":"d-selected.execute-api.us-east-1.amazonaws.com","hostedZoneId":"ZAPIGATEWAY","endpointType":"REGIONAL","securityPolicy":"TLS_1_2"}]},{"domainName":"fixture.example.com","tags":{}}]}`, tag)
				case "/v2/domainnames/api.example.com/apimappings":
					mappingReads++
					body = `{"items":[{"apiMappingId":"mapping","apiId":"selected","apiMappingKey":"","stage":"$default"}]}`
				case "/2013-04-01/hostedzone/ZSELECTED/rrset":
					dnsReads++
					if r.URL.Query().Get("name") != "api.example.com" || r.URL.Query().Get("type") != "A" || r.URL.Query().Get("maxitems") != "1" {
						return nil, fmt.Errorf("unbounded DNS lookup: %s", r.URL)
					}
					body = `<ListResourceRecordSetsResponse><ResourceRecordSets><ResourceRecordSet><Name>api.example.com.</Name><Type>A</Type><AliasTarget><HostedZoneId>ZAPIGATEWAY</HostedZoneId><DNSName>d-selected.execute-api.us-east-1.amazonaws.com.</DNSName><EvaluateTargetHealth>false</EvaluateTargetHealth></AliasTarget></ResourceRecordSet></ResourceRecordSets><IsTruncated>false</IsTruncated></ListResourceRecordSetsResponse>`
				case "/v2/apis/selected/integrations":
					body = `{"items":[{}]}`
				default:
					return nil, fmt.Errorf("unrelated resource hydration: %s", r.URL)
				}
				return infraSetResponse(r, 200, body), nil
			}))
			triggers := make(chan *InfraTrigger, 1)
			apis, err := (infraListScope{setName: "wanted"}).listApi(context.Background(), triggers)
			if err != nil || len(apis) != 1 || apis["cross-named-function"] == nil {
				t.Fatalf("selected API: apis=%v err=%v", apis, err)
			}
			if ownedDomain {
				if mappingReads != 1 || dnsReads != 1 || apis["cross-named-function"].Dns != "api.example.com" {
					t.Fatalf("owned domain not reconstructed: api=%+v mappings=%d dns=%d", apis, mappingReads, dnsReads)
				}
			} else if mappingReads != 0 || dnsReads != 0 || apis["cross-named-function"].Dns != "" {
				t.Fatalf("unrelated domain inspected: mappings=%d dns=%d", mappingReads, dnsReads)
			}
			if len(triggers) != 1 || (<-triggers).lambdaName != "cross-named-function" {
				t.Fatal("selected API trigger lost")
			}
		})
	}
}

func TestInfraListLambdaReturnsDiscoveryErrorWithoutTriggerProducer(t *testing.T) {
	installInfraSetClients(t, infraSetTransport(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("function discovery probe")
	}))
	if _, err := InfraListLambda(context.Background(), nil, ""); err == nil || !strings.Contains(err.Error(), "function discovery probe") {
		t.Fatalf("discovery error not returned: %v", err)
	}
}
