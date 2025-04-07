package lib

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/acm"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	apitypes "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	eventbridgetypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	route53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/ses"
)

const (
	lambdaAttrConcurrency = "concurrency"
	lambdaAttrMemory      = "memory"
	lambdaAttrTimeout     = "timeout"
	lambdaAttrLogsTTLDays = "logs-ttl-days"

	lambdaAttrConcurrencyDefault = 0
	lambdaAttrMemoryDefault      = 128
	lambdaAttrTimeoutDefault     = 300
	lambdaAttrLogsTTLDaysDefault = 7

	lambdaTriggerSes           = "ses"
	lambdaTriggerSesAttrDns    = "dns"
	lambdaTriggerSesAttrBucket = "bucket"
	lambdaTriggerSesAttrPrefix = "prefix"

	lambdaTriggerSQS       = "sqs"
	lambdaTrigerS3         = "s3"
	lambdaTriggerDynamoDB  = "dynamodb"
	lambdaTriggerSchedule  = "schedule"
	lambdaTriggerEcr       = "ecr"
	lambdaTriggerApi       = "api"
	lambdaTriggerWebsocket = "websocket"

	lambdaTriggerApiAttrDns    = "dns"
	lambdaTriggerApiAttrDomain = "domain"

	lambdaDollarDefault     = "$default"
	lambdaDollarConnect     = "$connect"
	lambdaDollarDisconnect  = "$disconnect"
	lambdaAuthorizationType = "NONE"
	lambdaRouteSelection    = "${request.body.action}"
	lambdaIntegrationMethod = "POST"
	lambdaPayloadVersion    = "1.0"

	lambdaEnvVarApiID       = "API_ID"
	lambdaEnvVarWebsocketID = "WEBSOCKET_ID"

	lambdaEventRuleNameSeparator = "___"
	LambdaWebsocketSuffix        = lambdaEventRuleNameSeparator + "websocket"

	lambdaRuntimePython    = "python3.13"
	lambdaRuntimeGo        = "provided.al2023"
	lambdaRuntimeContainer = "container"
)

var lambdaClient *lambda.Client
var lambdaClientLock sync.Mutex

func LambdaClientExplicit(accessKeyID, accessKeySecret, region string) *lambda.Client {
	return lambda.NewFromConfig(*SessionExplicit(accessKeyID, accessKeySecret, region))
}

func LambdaClient() *lambda.Client {
	lambdaClientLock.Lock()
	defer lambdaClientLock.Unlock()
	if lambdaClient == nil {
		lambdaClient = lambda.NewFromConfig(*Session())
	}
	return lambdaClient
}

func LambdaSetConcurrency(ctx context.Context, lambdaName string, concurrency int, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaSetConcurrency"}
		defer d.Log()
	}
	out, err := LambdaClient().GetFunctionConcurrency(ctx, &lambda.GetFunctionConcurrencyInput{
		FunctionName: aws.String(lambdaName),
	})
	if err != nil {
		if !preview {
			Logger.Println("error:", err)
			return err
		}
		out = &lambda.GetFunctionConcurrencyOutput{}
	}
	if out.ReservedConcurrentExecutions == nil {
		out.ReservedConcurrentExecutions = aws.Int32(0)
	}
	if int(*out.ReservedConcurrentExecutions) != concurrency {
		if !preview {
			if concurrency > 0 {
				_, err := LambdaClient().PutFunctionConcurrency(ctx, &lambda.PutFunctionConcurrencyInput{
					FunctionName:                 aws.String(lambdaName),
					ReservedConcurrentExecutions: aws.Int32(int32(concurrency)),
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			} else {
				_, err := LambdaClient().DeleteFunctionConcurrency(ctx, &lambda.DeleteFunctionConcurrencyInput{
					FunctionName: aws.String(lambdaName),
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
		}
		Logger.Printf(PreviewString(preview)+"updated concurrency: %d => %d\n", *out.ReservedConcurrentExecutions, concurrency)
	}
	return nil
}

func LambdaArn(ctx context.Context, name string) (string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaArn"}
		defer d.Log()
	}
	var expectedErr error
	var arn string
	err := Retry(ctx, func() error {
		out, err := LambdaClient().GetFunction(ctx, &lambda.GetFunctionInput{
			FunctionName: aws.String(name),
		})
		if err != nil {
			var notFound *lambdatypes.ResourceNotFoundException
			if errors.As(err, &notFound) {
				expectedErr = err
				return nil
			}
			return err
		}
		arn = *out.Configuration.FunctionArn
		return nil
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	if expectedErr != nil {
		return "", expectedErr
	}
	return arn, nil
}

const lambdaEcrEventPattern = `{
  "source": ["aws.ecr"],
  "detail-type": ["ECR Image Action"],
  "detail": {
    "result": ["SUCCESS"]
  }
}`

func LambdaEnsureTriggerSes(ctx context.Context, infraLambda *InfraLambda, preview bool) (string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaEnsureTriggerSes"}
		defer d.Log()
	}
	var triggers []*InfraTrigger
	for _, trigger := range infraLambda.Trigger {
		if trigger.Type == lambdaTriggerSes {
			triggers = append(triggers, trigger)
			break
		}
	}
	sid := ""
	domains := []string{}
	for _, trigger := range triggers {
		domainName := ""
		bucket := ""
		prefix := ""
		for _, attr := range trigger.Attr {
			k, v, err := SplitOnce(attr, "=")
			if err != nil {
				Logger.Println("error:", err)
				return "", err
			}
			switch k {
			case lambdaTriggerSesAttrDns: // domain already enrolled in ses, ie example.com
				domainName = v
			case lambdaTriggerSesAttrBucket:
				bucket = v
			case lambdaTriggerSesAttrPrefix:
				prefix = v
			}
		}
		if domainName == "" {
			return "", fmt.Errorf("ses trigger needs dns=VALUE")
		}
		if bucket == "" {
			return "", fmt.Errorf("ses trigger needs bucket=VALUE")
		}
		domains = append(domains, domainName)
		var err error
		sid, err = SesEnsureReceiptRuleset(ctx, domainName, bucket, prefix, infraLambda.Arn, preview)
		if err != nil {
			Logger.Println("error:", err)
			return "", err
		}
	}
	rules, err := SesListReceiptRulesets(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	for _, rule := range rules {
		out, err := SesClient().DescribeReceiptRuleSet(ctx, &ses.DescribeReceiptRuleSetInput{
			RuleSetName: rule.Name,
		})
		if err != nil {
			Logger.Println("error:", err)
			return "", err
		}
		if len(out.Rules) != 1 {
			err := fmt.Errorf("ses rule did not have exactly 1 rule: %s", *rule.Name)
			Logger.Println("error:", err)
			return "", err
		}
		if len(out.Rules[0].Actions) != 2 {
			err := fmt.Errorf("ses rule did not have exactly 2 actions: %s", *rule.Name)
			Logger.Println("error:", err)
			return "", err
		}
		if *out.Rules[0].Actions[1].LambdaAction.FunctionArn != infraLambda.Arn {
			continue
		}
		if !Contains(domains, *rule.Name) {
			err := SesRmReceiptRuleset(ctx, *rule.Name, preview)
			if err != nil {
				Logger.Println("error:", err)
				return "", err
			}
		}
	}
	return sid, nil
}

func LambdaEnsureTriggerEcr(ctx context.Context, infraLambda *InfraLambda, preview bool) ([]string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaEnsureTriggerEcr"}
		defer d.Log()
	}
	ruleName := fmt.Sprintf("%s%strigger_ecr", infraLambda.Name, lambdaEventRuleNameSeparator)
	var permissionSids []string
	var triggers []string
	for _, trigger := range infraLambda.Trigger {
		if trigger.Type == lambdaTriggerEcr {
			triggers = append(triggers, trigger.Type)
			break
		}
	}
	if len(triggers) > 0 {
		var ruleArn string
		out, err := EventsClient().DescribeRule(ctx, &eventbridge.DescribeRuleInput{
			Name: aws.String(ruleName),
		})
		if err != nil {
			var rnfe *eventbridgetypes.ResourceNotFoundException
			if !errors.As(err, &rnfe) {
				return nil, err
			}
			if !preview {
				out, err := EventsClient().PutRule(ctx, &eventbridge.PutRuleInput{
					Name:         aws.String(ruleName),
					EventPattern: aws.String(lambdaEcrEventPattern),
					Tags: []eventbridgetypes.Tag{{
						Key:   aws.String(infraSetTagName),
						Value: aws.String(infraLambda.infraSetName),
					}},
				})
				if err != nil {
					Logger.Println("error:", err)
					return nil, err
				}
				ruleArn = *out.RuleArn
			}
			Logger.Println(PreviewString(preview)+"created ecr rule:", ruleName)
		} else {
			if *out.EventPattern != lambdaEcrEventPattern {
				err := fmt.Errorf("ecr rule misconfigured: %s %s != %s", ruleName, lambdaEcrEventPattern, *out.EventPattern)
				Logger.Println("error:", err)
				return nil, err
			}
			ruleArn = *out.Arn
		}
		sid, err := lambdaEnsurePermission(ctx, infraLambda.Name, "events.amazonaws.com", ruleArn, preview)
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		permissionSids = append(permissionSids, sid)
		var targets []eventbridgetypes.Target
		err = Retry(ctx, func() error {
			var err error
			targets, err = EventsListRuleTargets(ctx, ruleName, nil)
			var rnfe *eventbridgetypes.ResourceNotFoundException
			if errors.As(err, &rnfe) {
				return nil
			}
			return err
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		switch len(targets) {
		case 0:
			if !preview {
				_, err := EventsClient().PutTargets(ctx, &eventbridge.PutTargetsInput{
					Rule: aws.String(ruleName),
					Targets: []eventbridgetypes.Target{{
						Id:  aws.String("1"),
						Arn: aws.String(infraLambda.Arn),
					}},
				})
				if err != nil {
					Logger.Println("error:", err)
					return nil, err
				}
			}
			Logger.Println(PreviewString(preview)+"created ecr rule target:", ruleName, infraLambda.Arn)
		case 1:
			if *targets[0].Arn != infraLambda.Arn {
				err := fmt.Errorf("ecr rule is misconfigured with unknown target: %s %s", infraLambda.Arn, *targets[0].Arn)
				Logger.Println("error:", err)
				return nil, err
			}
		default:
			var targetArns []string
			for _, target := range targets {
				targetArns = append(targetArns, *target.Arn)
			}
			err := fmt.Errorf("ecr rule is misconfigured with unknown targets: %s %v", infraLambda.Arn, targetArns)
			Logger.Println("error:", err)
			return nil, err
		}
	} else {
		rules, err := EventsListRules(ctx, nil)
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		for _, rule := range rules {
			targets, err := EventsListRuleTargets(ctx, *rule.Name, nil)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			for _, target := range targets {
				if *target.Arn == infraLambda.Arn && rule.EventPattern != nil && *rule.EventPattern == lambdaEcrEventPattern {
					if !preview {
						ids := []string{}
						for _, target := range targets {
							ids = append(ids, *target.Id)
						}
						_, err := EventsClient().RemoveTargets(ctx, &eventbridge.RemoveTargetsInput{
							Rule: rule.Name,
							Ids:  ids,
						})
						if err != nil {
							Logger.Println("error:", err)
							return nil, err
						}
						_, err = EventsClient().DeleteRule(ctx, &eventbridge.DeleteRuleInput{
							Name: rule.Name,
						})
						if err != nil {
							Logger.Println("error:", err)
							return nil, err
						}
					}
					Logger.Println(PreviewString(preview)+"deleted ecr trigger:", infraLambda.Name)
					break
				}
			}
		}
	}
	return permissionSids, nil
}

func LambdaEnsureTriggerS3(ctx context.Context, infraLambda *InfraLambda, preview bool) ([]string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaEnsureTriggerS3"}
		defer d.Log()
	}
	var permissionSids []string
	events := []s3types.Event{
		"s3:ObjectCreated:*",
		"s3:ObjectRemoved:*",
	}
	var triggers []string
	for _, trigger := range infraLambda.Trigger {
		if trigger.Type == lambdaTrigerS3 {
			triggers = append(triggers, trigger.Attr[0])
		}
	}
	if len(triggers) > 0 {
		for _, bucket := range triggers {
			sid, err := lambdaEnsurePermission(ctx, infraLambda.Name, "s3.amazonaws.com", "arn:aws:s3:::"+bucket, preview)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			permissionSids = append(permissionSids, sid)
			s3Client, err := S3ClientBucketRegion(bucket)
			if err != nil {
				var noBucket *s3types.NoSuchBucket
				if !errors.As(err, &noBucket) && !preview {
					Logger.Println("error:", err)
					return nil, err
				}
				s3Client = nil
			}
			var out *s3.GetBucketNotificationConfigurationOutput
			if s3Client != nil {
				getOut, err := s3Client.GetBucketNotificationConfiguration(ctx, &s3.GetBucketNotificationConfigurationInput{
					Bucket: aws.String(bucket),
				})
				if err != nil {
					var noBucket *s3types.NoSuchBucket
					if !errors.As(err, &noBucket) {
						Logger.Println("error:", err)
						return nil, err
					}
				} else {
					out = getOut
				}
			}
			if out == nil {
				out = &s3.GetBucketNotificationConfigurationOutput{
					LambdaFunctionConfigurations: []s3types.LambdaFunctionConfiguration{},
				}
			}
			var existingEvents []s3types.Event
			for _, conf := range out.LambdaFunctionConfigurations {
				if *conf.LambdaFunctionArn == infraLambda.Arn {
					existingEvents = conf.Events
				}
			}
			if !reflect.DeepEqual(existingEvents, events) {
				var confs []s3types.LambdaFunctionConfiguration
				for _, conf := range out.LambdaFunctionConfigurations {
					if *conf.LambdaFunctionArn != infraLambda.Arn {
						confs = append(confs, conf)
					}
				}
				confs = append(confs, s3types.LambdaFunctionConfiguration{
					LambdaFunctionArn: aws.String(infraLambda.Arn),
					Events:            events,
				})
				if !preview && s3Client != nil {
					err := Retry(ctx, func() error {
						_, err := s3Client.PutBucketNotificationConfiguration(ctx, &s3.PutBucketNotificationConfigurationInput{
							Bucket: aws.String(bucket),
							NotificationConfiguration: &s3types.NotificationConfiguration{
								LambdaFunctionConfigurations: confs,
								EventBridgeConfiguration:     out.EventBridgeConfiguration,
								QueueConfigurations:          out.QueueConfigurations,
								TopicConfigurations:          out.TopicConfigurations,
							},
						})
						return err
					})
					if err != nil {
						Logger.Println("error:", err)
						return nil, err
					}
				}
				Logger.Printf(PreviewString(preview)+"updated bucket notifications for %s %s: %s => %s\n",
					bucket, infraLambda.Name, existingEvents, events)
			}
		}
	}
	buckets, err := S3Client().ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	for _, bucket := range buckets.Buckets {
		out, err := S3ClientBucketRegionMust(*bucket.Name).GetBucketNotificationConfiguration(ctx, &s3.GetBucketNotificationConfigurationInput{
			Bucket: bucket.Name,
		})
		if err != nil {
			var noBucket *s3types.NoSuchBucket
			if errors.As(err, &noBucket) {
				continue // recently delete buckets can still show up in listbuckets but fail with 404
			}
			Logger.Println("error:", err)
			return nil, err
		}
		var confs []s3types.LambdaFunctionConfiguration
		for _, conf := range out.LambdaFunctionConfigurations {
			if *conf.LambdaFunctionArn != infraLambda.Arn || Contains(triggers, *bucket.Name) {
				confs = append(confs, conf)
			} else {
				Logger.Println(PreviewString(preview)+"deleted bucket notification:", infraLambda.Name, *bucket.Name)
			}
		}
		if len(confs) != len(out.LambdaFunctionConfigurations) && !preview {
			_, err := S3ClientBucketRegionMust(*bucket.Name).PutBucketNotificationConfiguration(ctx, &s3.PutBucketNotificationConfigurationInput{
				Bucket: bucket.Name,
				NotificationConfiguration: &s3types.NotificationConfiguration{
					LambdaFunctionConfigurations: confs,
					EventBridgeConfiguration:     out.EventBridgeConfiguration,
					QueueConfigurations:          out.QueueConfigurations,
					TopicConfigurations:          out.TopicConfigurations,
				},
			})
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
		}
	}
	return permissionSids, nil
}

func lambdaRemoveUnusedPermissions(ctx context.Context, name string, permissionSids []string, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaRemoveUnusedPermissions"}
		defer d.Log()
	}
	out, err := LambdaClient().GetPolicy(ctx, &lambda.GetPolicyInput{
		FunctionName: aws.String(name),
	})
	if err != nil {
		var notFound *lambdatypes.ResourceNotFoundException
		if !errors.As(err, &notFound) {
			Logger.Println("error:", err)
			return err
		}
		return nil
	}
	policy := IamPolicyDocument{}
	err = json.Unmarshal([]byte(*out.Policy), &policy)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	for _, statement := range policy.Statement {
		if !Contains(permissionSids, statement.Sid) {
			if !preview {
				_, err := LambdaClient().RemovePermission(ctx, &lambda.RemovePermissionInput{
					FunctionName: aws.String(name),
					StatementId:  aws.String(statement.Sid),
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Println(PreviewString(preview)+"deleted unused lambda permissions:", name, statement.Sid)
		}
	}
	return nil
}

func lambdaAddPermission(ctx context.Context, sid, name, callerPrincipal, callerArn string) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaAddPermission"}
		defer d.Log()
	}
	region := Region()
	account, err := StsAccount(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	_, err = LambdaClient().AddPermission(ctx, &lambda.AddPermissionInput{
		FunctionName: aws.String(fmt.Sprintf("arn:aws:lambda:%s:%s:function:%s", region, account, name)),
		StatementId:  aws.String(sid),
		Action:       aws.String("lambda:InvokeFunction"),
		Principal:    aws.String(callerPrincipal),
		SourceArn:    aws.String(callerArn),
	})
	return err
}

func lambdaEnsurePermission(ctx context.Context, name, callerPrincipal, callerArn string, preview bool) (string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaEnsurePermission"}
		defer d.Log()
	}
	sid := strings.ReplaceAll(callerPrincipal, ".", "-") + "__" + Last(strings.Split(callerArn, ":"))
	sid = strings.ReplaceAll(sid, "$", "DOLLAR")
	sid = strings.ReplaceAll(sid, "*", "ALL")
	sid = strings.ReplaceAll(sid, ".", "DOT")
	sid = strings.ReplaceAll(sid, "-", "_")
	sid = strings.ReplaceAll(sid, "/", "__")
	var expectedErr error
	var policyString string
	err := Retry(ctx, func() error {
		out, err := LambdaClient().GetPolicy(ctx, &lambda.GetPolicyInput{
			FunctionName: aws.String(name),
		})
		if err != nil {
			var notFound *lambdatypes.ResourceNotFoundException
			if errors.As(err, &notFound) {
				expectedErr = err
				return nil
			}
			return err
		}
		policyString = *out.Policy
		return nil
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	if expectedErr != nil {
		if !preview {
			err := lambdaAddPermission(ctx, sid, name, callerPrincipal, callerArn)
			if err != nil {
				Logger.Println("error:", err)
				return "", err
			}
		}
		Logger.Println(PreviewString(preview)+"created lambda permission:", name, callerPrincipal, callerArn)
		return sid, nil
	}
	needsUpdate := true
	if policyString != "" {
		policy := IamPolicyDocument{}
		err := json.Unmarshal([]byte(policyString), &policy)
		if err != nil {
			Logger.Println("error:", err)
			return "", err
		}
		for _, statement := range policy.Statement {
			if statement.Sid == sid {
				needsUpdate = false
				break
			}
		}
	}
	if needsUpdate {
		if !preview {
			err := lambdaAddPermission(ctx, sid, name, callerPrincipal, callerArn)
			if err != nil {
				Logger.Println("error:", err)
				return "", err
			}
		}
		Logger.Println(PreviewString(preview)+"updated lambda permission:", name, callerPrincipal, callerArn)
		return sid, nil
	}
	return sid, nil
}

func LambdaArnToLambdaName(arn string) string {
	// "arn:aws:lambda:%s:%s:function:%s"
	name := Last(strings.Split(arn, ":"))
	return name
}

func lambdaEnsureTriggerApi(ctx context.Context, infraSetName, apiName, arnLambda string, protocolType apitypes.ProtocolType, preview bool) (*apitypes.Api, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaEnsureTriggerApi"}
		defer d.Log()
	}
	var pType apitypes.ProtocolType
	if !Contains(pType.Values(), protocolType) {
		err := fmt.Errorf("invalid protocol type: %s", protocolType)
		Logger.Println("error:", err)
		return nil, err
	}
	api, err := Api(ctx, apiName)
	if err != nil && err.Error() != ErrApiNotFound {
		Logger.Println("error:", err)
		return nil, err
	}
	if api == nil {
		if !preview {
			input := &apigatewayv2.CreateApiInput{
				Name:         aws.String(apiName),
				ProtocolType: apitypes.ProtocolType(protocolType),
				Tags: map[string]string{
					infraSetTagName: infraSetName,
				},
			}
			if protocolType == apitypes.ProtocolTypeWebsocket {
				input.RouteKey = aws.String(lambdaDollarDefault)
				input.Target = aws.String(arnLambda)
				input.RouteSelectionExpression = aws.String(lambdaRouteSelection)
			}
			_, err := ApiClient().CreateApi(ctx, input)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			api, err := Api(ctx, apiName)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			return api, nil
		}
		Logger.Println(PreviewString(preview)+"created api:", apiName)
		return nil, nil
	}
	if api.ProtocolType != apitypes.ProtocolType(protocolType) {
		err := fmt.Errorf("api protocol type misconfigured for %s %s: %v != %v", apiName, *api.ApiId, api.ProtocolType, protocolType)
		Logger.Println("error:", err)
		return nil, err
	}
	return api, nil
}

func lambdaEnsureTriggerApiIntegrationStageRoute(ctx context.Context, name, arnLambda string, protocolType apitypes.ProtocolType, api *apitypes.Api, timeoutMillis int32, preview bool) (string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaEnsureTriggerApiIntegrationStageRoute"}
		defer d.Log()
	}
	if api == nil && preview {
		Logger.Println(PreviewString(preview)+"created api integration:", name)
		Logger.Println(PreviewString(preview)+"created api stage:", name)
		Logger.Println(PreviewString(preview)+"created api route:", name)
		return "", nil
	}
	account, err := StsAccount(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	var integrationId string
	var getIntegrationsOut *apigatewayv2.GetIntegrationsOutput
	if !(preview && api == nil) {
		getIntegrationsOut, err = ApiClient().GetIntegrations(ctx, &apigatewayv2.GetIntegrationsInput{
			ApiId:      api.ApiId,
			MaxResults: aws.String(fmt.Sprint(500)),
		})
		if err != nil {
			Logger.Println("error:", err)
			return "", err
		}
		if len(getIntegrationsOut.Items) == 500 {
			err := fmt.Errorf("too many integrations for %s %s", name, *api.ApiId)
			Logger.Println("error:", err)
			return "", err
		}
	}
	switch len(getIntegrationsOut.Items) {
	case 0:
		if !preview {
			out, err := ApiClient().CreateIntegration(ctx, &apigatewayv2.CreateIntegrationInput{
				ApiId:                api.ApiId,
				IntegrationUri:       aws.String(arnLambda),
				ConnectionType:       apitypes.ConnectionTypeInternet,
				IntegrationType:      apitypes.IntegrationTypeAwsProxy,
				IntegrationMethod:    aws.String(lambdaIntegrationMethod),
				TimeoutInMillis:      aws.Int32(timeoutMillis),
				PayloadFormatVersion: aws.String(lambdaPayloadVersion),
			})
			if err != nil {
				Logger.Println("error:", err)
				return "", err
			}
			integrationId = *out.IntegrationId
		}
		Logger.Println(PreviewString(preview)+"created api integration:", name)
	case 1:
		integration := getIntegrationsOut.Items[0]
		integrationId = *integration.IntegrationId
		if integration.ConnectionType != apitypes.ConnectionTypeInternet {
			err := fmt.Errorf("api connection type misconfigured for %s %s: %s != %s", name, *api.ApiId, integration.ConnectionType, apitypes.ConnectionTypeInternet)
			Logger.Println("error:", err)
			return "", err
		}
		if integration.IntegrationType != apitypes.IntegrationTypeAwsProxy {
			err := fmt.Errorf("api integration type misconfigured for %s %s: %s != %s", name, *api.ApiId, integration.IntegrationType, apitypes.IntegrationTypeAwsProxy)
			Logger.Println("error:", err)
			return "", err
		}
		if *integration.IntegrationMethod != lambdaIntegrationMethod {
			err := fmt.Errorf("api integration method misconfigured for %s %s: %s != %s", name, *api.ApiId, *integration.IntegrationMethod, lambdaIntegrationMethod)
			Logger.Println("error:", err)
			return "", err
		}
		if *integration.TimeoutInMillis != timeoutMillis {
			err := fmt.Errorf("api timeout misconfigured for %s %s: %d != %d", name, *api.ApiId, *integration.TimeoutInMillis, timeoutMillis)
			Logger.Println("error:", err)
			return "", err
		}
		if *integration.PayloadFormatVersion != lambdaPayloadVersion {
			err := fmt.Errorf("api payload format version misconfigured for %s %s: %s != %s", name, *api.ApiId, *integration.PayloadFormatVersion, lambdaPayloadVersion)
			Logger.Println("error:", err)
			return "", err
		}
	default:
		err := fmt.Errorf("api has more than one integration: %s %v", name, Pformat(getIntegrationsOut.Items))
		Logger.Println("error:", err)
		return "", err
	}
	getStageOut, err := ApiClient().GetStage(ctx, &apigatewayv2.GetStageInput{
		ApiId:     api.ApiId,
		StageName: aws.String(lambdaDollarDefault),
	})
	if err != nil {
		var nfe *apitypes.NotFoundException
		if !errors.As(err, &nfe) {
			Logger.Println("error:", err)
			return "", err
		}
		if !preview {
			_, err := ApiClient().CreateStage(ctx, &apigatewayv2.CreateStageInput{
				ApiId:      api.ApiId,
				AutoDeploy: aws.Bool(true),
				StageName:  aws.String(lambdaDollarDefault),
			})
			if err != nil {
				Logger.Println("error:", err)
				return "", err
			}
		}
		Logger.Println(PreviewString(preview)+"created api stage:", name)
	} else {
		if *getStageOut.StageName != lambdaDollarDefault {
			err := fmt.Errorf("api stage name misconfigured for %s %s: %s != %s", name, *api.ApiId, *getStageOut.StageName, lambdaDollarDefault)
			Logger.Println("error:", err)
			return "", err
		}
		if !*getStageOut.AutoDeploy {
			err := fmt.Errorf("api stage auto deploy misconfigured for %s %s, should be enabled", name, *api.ApiId)
			Logger.Println("error:", err)
			return "", err
		}
	}
	getRoutesOut, err := ApiClient().GetRoutes(ctx, &apigatewayv2.GetRoutesInput{
		ApiId:      api.ApiId,
		MaxResults: aws.String(fmt.Sprint(500)),
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	if len(getRoutesOut.Items) == 500 {
		err := fmt.Errorf("api has 500 or more routes: %s %s", name, *api.ApiId)
		Logger.Println("error:", err)
		return "", err
	}
	var routeKeys []string
	if protocolType == apitypes.ProtocolTypeHttp {
		routeKeys = []string{lambdaDollarDefault}
	} else if protocolType == apitypes.ProtocolTypeWebsocket {
		routeKeys = []string{lambdaDollarDefault, lambdaDollarConnect, lambdaDollarDisconnect}
	}
	for _, routeKey := range routeKeys {
		var routes []apitypes.Route
		for _, route := range getRoutesOut.Items {
			if *route.RouteKey == routeKey {
				routes = append(routes, route)
			}
		}
		switch len(routes) {
		case 0:
			if !preview {
				_, err := ApiClient().CreateRoute(ctx, &apigatewayv2.CreateRouteInput{
					ApiId:             api.ApiId,
					Target:            aws.String(fmt.Sprintf("integrations/%s", integrationId)),
					RouteKey:          aws.String(routeKey),
					AuthorizationType: apitypes.AuthorizationType(lambdaAuthorizationType),
					ApiKeyRequired:    aws.Bool(false),
				})
				if err != nil {
					Logger.Println("error:", err)
					return "", err
				}
			}
			Logger.Println(PreviewString(preview)+"created api route:", name, routeKey)
		case 1:
			route := routes[0]
			if *route.Target != fmt.Sprintf("integrations/%s", integrationId) {
				err := fmt.Errorf("api route target misconfigured for %s %s: %s != %s", name, *api.ApiId, *route.Target, fmt.Sprintf("integrations/%s", integrationId))
				Logger.Println("error:", err)
				return "", err
			}
			if *route.RouteKey != routeKey {
				err := fmt.Errorf("api route key misconfigured for %s %s: %s != %s", name, *api.ApiId, *route.RouteKey, routeKey)
				Logger.Println("error:", err)
				return "", err
			}
			if route.AuthorizationType != apitypes.AuthorizationType(lambdaAuthorizationType) {
				err := fmt.Errorf("api route authorization type misconfigured for %s %s: %s != %s", name, *api.ApiId, route.AuthorizationType, lambdaAuthorizationType)
				Logger.Println("error:", err)
				return "", err
			}
			if *route.ApiKeyRequired {
				err := fmt.Errorf("api route apiKeyRequired misconfigured for %s %s, should be disabled", name, *api.ApiId)
				Logger.Println("error:", err)
				return "", err
			}
		default:
			err := fmt.Errorf("api has more than one route: %s %s %v", name, routeKey, Pformat(routes))
			Logger.Println("error:", err)
			return "", err
		}
	}
	arn := fmt.Sprintf("arn:aws:execute-api:%s:%s:%s/*/*", Region(), account, *api.ApiId)
	lambdaName := Last(strings.Split(arnLambda, ":"))
	sid, err := lambdaEnsurePermission(ctx, lambdaName, "apigateway.amazonaws.com", arn, preview)
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	return sid, nil
}

func lambdaEnsureTriggerApiDomainName(ctx context.Context, name, domain string, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaEnsureTriggerApiDomainName"}
		defer d.Log()
	}
	out, err := ApiClient().GetDomainName(ctx, &apigatewayv2.GetDomainNameInput{
		DomainName: aws.String(domain),
	})
	if err != nil {
		var nfe *apitypes.NotFoundException
		if !errors.As(err, &nfe) {
			Logger.Println("error:", err)
			return err
		}
		certs, err := AcmListCertificates(ctx)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		arnCert := ""
		for _, cert := range certs {
			if *cert.DomainName == domain {
				arnCert = *cert.CertificateArn
				break
			}
		}
		if arnCert == "" {
			_, parentDomain, err := SplitOnce(domain, ".")
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			certs, err := AcmListCertificates(ctx)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			for _, cert := range certs {
				descOut, err := AcmClient().DescribeCertificate(ctx, &acm.DescribeCertificateInput{
					CertificateArn: cert.CertificateArn,
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				wildcard := fmt.Sprintf("*.%s", parentDomain)
				if Contains(descOut.Certificate.SubjectAlternativeNames, wildcard) {
					arnCert = *cert.CertificateArn
					break
				}
			}
		}
		if arnCert == "" {
			err := fmt.Errorf("no acm cert found for: %s", domain)
			Logger.Println("error:", err)
			return err
		}
		if !preview {
			var unexpectedErr error
			err := Retry(ctx, func() error {
				_, err := ApiClient().CreateDomainName(ctx, &apigatewayv2.CreateDomainNameInput{
					DomainName: aws.String(domain),
					DomainNameConfigurations: []apitypes.DomainNameConfiguration{{
						ApiGatewayDomainName: aws.String(domain),
						CertificateArn:       aws.String(arnCert),
						EndpointType:         apitypes.EndpointTypeRegional,
						SecurityPolicy:       apitypes.SecurityPolicyTls12,
					}},
				})
				if err != nil {
					if strings.Contains(err.Error(), "TooManyRequestsException") {
						Logger.Println("create domain has low rate limits, sleeping then retrying")
						time.Sleep(15 * time.Second)
					} else {
						unexpectedErr = err
						return nil
					}
				}
				return err
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			if unexpectedErr != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"created api domain:", name, domain)
	} else {
		if len(out.DomainNameConfigurations) != 1 || out.DomainNameConfigurations[0].EndpointType != apitypes.EndpointTypeRegional {
			err := fmt.Errorf("api endpoint type misconfigured: %s", Pformat(out.DomainNameConfigurations))
			Logger.Println("error:", err)
			return err
		}
		if out.DomainNameConfigurations[0].SecurityPolicy == "" || out.DomainNameConfigurations[0].SecurityPolicy != apitypes.SecurityPolicyTls12 {
			err := fmt.Errorf("api security policy misconfigured: %s", out.DomainNameConfigurations[0].SecurityPolicy)
			Logger.Println("error:", err)
			return err
		}
	}
	return nil
}

func lambdaEnsureTriggerApiDnsRecords(ctx context.Context, name, subDomain string, zone route53types.HostedZone, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaEnsureTriggerApiDnsRecords"}
		defer d.Log()
	}
	out, err := ApiClient().GetDomainName(ctx, &apigatewayv2.GetDomainNameInput{
		DomainName: aws.String(subDomain),
	})
	if err != nil {
		var nfe *apitypes.NotFoundException
		if !errors.As(err, &nfe) {
			Logger.Println("error:", err)
			return err
		}
		Logger.Println(PreviewString(preview)+"created api dns:", name, subDomain)
	} else {
		records, err := Route53ListRecords(ctx, *zone.Id)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		found := false
		needsUpdate := false
		for _, record := range records {
			if strings.TrimRight(*record.Name, ".") == subDomain && record.Type == route53types.RRTypeA {
				found = true
				if strings.TrimRight(*record.AliasTarget.DNSName, ".") != *out.DomainNameConfigurations[0].ApiGatewayDomainName {
					needsUpdate = true
					break
				}
			}
		}
		if !found || needsUpdate {
			if !preview {
				_, err := Route53Client().ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
					HostedZoneId: zone.Id,
					ChangeBatch: &route53types.ChangeBatch{
						Changes: []route53types.Change{{
							Action: route53types.ChangeActionUpsert,
							ResourceRecordSet: &route53types.ResourceRecordSet{
								Name: aws.String(subDomain),
								Type: route53types.RRTypeA,
								AliasTarget: &route53types.AliasTarget{
									DNSName:              out.DomainNameConfigurations[0].ApiGatewayDomainName,
									HostedZoneId:         out.DomainNameConfigurations[0].HostedZoneId,
									EvaluateTargetHealth: false,
								},
							},
						}},
					},
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			if needsUpdate {
				Logger.Println(PreviewString(preview)+"updated api dns:", name, subDomain)
			} else {
				Logger.Println(PreviewString(preview)+"created api dns:", name, subDomain)
			}
		}
	}
	return nil
}

func lambdaEnsureTriggerApiDns(ctx context.Context, name, domain string, api *apitypes.Api, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaEnsureTriggerApiDns"}
		defer d.Log()
	}
	zones, err := Route53ListZones(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	found := false
	for _, zone := range zones {
		if domain == strings.TrimRight(*zone.Name, ".") {
			found = true
			err := lambdaEnsureTriggerApiDomainName(ctx, name, domain, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			err = lambdaEnsureTriggerApiDnsRecords(ctx, name, domain, zone, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			err = lambdaEnsureTriggerApiMapping(ctx, name, domain, api, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			break
		}
	}
	if !found {
		_, parentDomain, err := SplitOnce(domain, ".")
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		subDomain := domain
		for _, zone := range zones {
			if parentDomain == strings.TrimRight(*zone.Name, ".") {
				found = true
				err := lambdaEnsureTriggerApiDomainName(ctx, name, subDomain, preview)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				err = lambdaEnsureTriggerApiDnsRecords(ctx, name, subDomain, zone, preview)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				err = lambdaEnsureTriggerApiMapping(ctx, name, subDomain, api, preview)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				break
			}
		}
	}
	if !found {
		err := fmt.Errorf("no zone found matching domain or parent domain: %s", domain)
		Logger.Println("error:", err)
		return err
	}
	return nil
}

func lambdaEnsureTriggerApiMapping(ctx context.Context, name, subDomain string, api *apitypes.Api, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaEnsureTriggerApiMapping"}
		defer d.Log()
	}
	if api == nil && preview {
		Logger.Println(PreviewString(preview)+"created api path mapping:", name, subDomain)
		return nil
	}
	mappings, err := ApiClient().GetApiMappings(ctx, &apigatewayv2.GetApiMappingsInput{
		DomainName: aws.String(subDomain),
		MaxResults: aws.String(fmt.Sprint(500)),
	})
	if err != nil {
		var nfe *apitypes.NotFoundException
		if !errors.As(err, &nfe) {
			Logger.Println("error:", err)
			return err
		}
	}
	if mappings != nil && len(mappings.Items) == 500 {
		err := fmt.Errorf("too many path mappings for domain %s", subDomain)
		Logger.Println("error:", err)
		return err
	}
	switch {
	case mappings == nil || len(mappings.Items) == 0:
		if !preview {
			_, err := ApiClient().CreateApiMapping(ctx, &apigatewayv2.CreateApiMappingInput{
				DomainName: aws.String(subDomain),
				ApiId:      api.ApiId,
				Stage:      aws.String(lambdaDollarDefault),
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"created api path mapping:", name, subDomain)
	case len(mappings.Items) == 1:
		mapping := mappings.Items[0]
		if *mapping.ApiId != *api.ApiId {
			err := fmt.Errorf("restapi id misconfigured: %s != %s", *mapping.ApiId, *api.ApiId)
			Logger.Println("error:", err)
			return err
		}
		if *mapping.Stage != lambdaDollarDefault {
			err := fmt.Errorf("stage misconfigured: %s != %s", *mapping.Stage, lambdaDollarDefault)
			Logger.Println("error:", err)
			return err
		}
	default:
		err := fmt.Errorf("found more than 1 path mapping: %s", Pformat(mappings.Items))
		Logger.Println("error:", err)
		return err
	}
	return nil
}

func LambdaEnsureTriggerApi(ctx context.Context, infraLambda *InfraLambda, preview bool) ([]string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaEnsureTriggerApi"}
		defer d.Log()
	}
	var permissionSids []string
	hasApi := false
	hasWebsocket := false
	domainApi := ""
	domainWebsocket := ""
	account, err := StsAccount(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	arnLambda := fmt.Sprintf("arn:aws:lambda:%s:%s:function:%s", Region(), account, infraLambda.Name)
	count := 0
	for _, trigger := range infraLambda.Trigger {
		var protocolType apitypes.ProtocolType
		var domainName string
		var apiName string
		var timeoutMillis int32
		if trigger.Type == lambdaTriggerApi || trigger.Type == lambdaTriggerWebsocket {
			time.Sleep(time.Duration(count) * 5 * time.Second)
			count++
			if trigger.Type == lambdaTriggerApi {
				apiName = infraLambda.Name
				if hasApi {
					err := fmt.Errorf("cannot have more than one api trigger")
					Logger.Println("error:", err)
					return nil, err
				}
				hasApi = true
				protocolType = apitypes.ProtocolTypeHttp
				timeoutMillis = 30000
			} else if trigger.Type == lambdaTriggerWebsocket {
				apiName = infraLambda.Name + LambdaWebsocketSuffix
				if hasWebsocket {
					err := fmt.Errorf("cannot have more than one websocket trigger")
					Logger.Println("error:", err)
					return nil, err
				}
				hasWebsocket = true
				protocolType = apitypes.ProtocolTypeWebsocket
				timeoutMillis = 29000
			}
			api, err := lambdaEnsureTriggerApi(ctx, infraLambda.infraSetName, apiName, arnLambda, protocolType, preview)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			if protocolType == apitypes.ProtocolTypeHttp {
				if api != nil && api.ApiId != nil {
					err := os.Setenv(lambdaEnvVarApiID, *api.ApiId)
					if err != nil {
						Logger.Println("error:", err)
						return nil, err
					}
				}
			} else if protocolType == apitypes.ProtocolTypeWebsocket {
				if api != nil && api.ApiId != nil {
					err := os.Setenv(lambdaEnvVarWebsocketID, *api.ApiId)
					if err != nil {
						Logger.Println("error:", err)
						return nil, err
					}
				}
			}
			sid, err := lambdaEnsureTriggerApiIntegrationStageRoute(ctx, apiName, arnLambda, protocolType, api, timeoutMillis, preview)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			permissionSids = append(permissionSids, sid)
			for _, attr := range trigger.Attr {
				k, v, err := SplitOnce(attr, "=")
				if err != nil {
					Logger.Println("error:", err)
					return nil, err
				}
				switch k {
				case lambdaTriggerApiAttrDns: // apigateway custom domain + route53
					domainName = v
					err := lambdaEnsureTriggerApiDns(ctx, apiName, domainName, api, preview)
					if err != nil {
						Logger.Println("error:", err)
						return nil, err
					}
				case lambdaTriggerApiAttrDomain: // apigateway custom domain
					domainName = v
					err := lambdaEnsureTriggerApiDomainName(ctx, apiName, domainName, preview)
					if err != nil {
						Logger.Println("error:", err)
						return nil, err
					}
					err = lambdaEnsureTriggerApiMapping(ctx, apiName, domainName, api, preview)
					if err != nil {
						Logger.Println("error:", err)
						return nil, err
					}
				default:
					err := fmt.Errorf("unknown attr: %s", attr)
					Logger.Println("error:", err)
					return nil, err
				}
				if trigger.Type == lambdaTriggerApi {
					domainApi = domainName
				} else if trigger.Type == lambdaTriggerWebsocket {
					domainWebsocket = domainName
				}
			}
		}
	}
	count = 0
	for _, kind := range []string{lambdaTriggerApi, lambdaTriggerWebsocket} {
		var apiEnsured bool
		var apiName string
		var apiDomain string
		if kind == lambdaTriggerApi {
			apiName = infraLambda.Name
			apiDomain = domainApi
			apiEnsured = hasApi
		} else {
			apiName = infraLambda.Name + LambdaWebsocketSuffix
			apiDomain = domainWebsocket
			apiEnsured = hasWebsocket
		}
		api, err := Api(ctx, apiName)
		if err != nil && err.Error() != ErrApiNotFound {
			Logger.Println("error:", err)
			return nil, err
		}
		if api != nil {
			time.Sleep(time.Duration(count) * 5 * time.Second) // very low api limits on apigateway delete domain, sleep here is we are adding more than 1
			count++
			domains, err := ApiListDomains(ctx)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			// delete any unused domains
			for _, domain := range domains {
				if *domain.DomainName == apiDomain {
					continue
				}
				err := lambdaTriggerApiDeleteDns(ctx, apiName, api, domain, preview)
				if err != nil {
					Logger.Println("error:", err)
					return nil, err
				}
			}
			// if api trigger unused, delete rest api
			if !apiEnsured {
				err := lambdaTriggerApiDeleteApi(ctx, apiName, api, preview)
				if err != nil {
					Logger.Println("error:", err)
					return nil, err
				}
			}
		}
	}
	return permissionSids, nil
}

func lambdaTriggerApiDeleteApi(ctx context.Context, name string, api *apitypes.Api, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaTriggerApiDeleteApi"}
		defer d.Log()
	}
	if !preview {
		_, err := ApiClient().DeleteApi(ctx, &apigatewayv2.DeleteApiInput{
			ApiId: api.ApiId,
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	Logger.Println(PreviewString(preview)+"deleted api trigger for:", name)
	return nil
}

func lambdaTriggerApiDeleteDns(ctx context.Context, name string, api *apitypes.Api, domain apitypes.DomainName, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaTriggerApiDeleteDns"}
		defer d.Log()
	}
	mappings, err := ApiClient().GetApiMappings(ctx, &apigatewayv2.GetApiMappingsInput{
		DomainName: domain.DomainName,
		MaxResults: aws.String(fmt.Sprint(500)),
	})
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if len(mappings.Items) == 500 {
		err := fmt.Errorf("too many api mappings for domain %s", *domain.DomainName)
		Logger.Println("error:", err)
		return err
	}
	for _, mapping := range mappings.Items {
		if *mapping.ApiId == *api.ApiId {
			zones, err := Route53ListZones(ctx)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			for _, zone := range zones {
				records, err := Route53ListRecords(ctx, *zone.Id)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				for _, record := range records {
					targetMatch := record.AliasTarget != nil &&
						record.AliasTarget.DNSName != nil &&
						domain.DomainNameConfigurations != nil &&
						len(domain.DomainNameConfigurations) > 0 &&
						strings.TrimRight(*record.AliasTarget.DNSName, ".") == *domain.DomainNameConfigurations[0].ApiGatewayDomainName
					nameMatch := record.Name != nil &&
						strings.TrimRight(*record.Name, ".") == *domain.DomainName
					if targetMatch && nameMatch {
						if !preview {
							_, err := Route53Client().ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
								HostedZoneId: zone.Id,
								ChangeBatch: &route53types.ChangeBatch{Changes: []route53types.Change{{
									Action:            route53types.ChangeActionDelete,
									ResourceRecordSet: &record,
								}}},
							})
							if err != nil {
								Logger.Println("error:", err)
								return err
							}
						}
						Logger.Println(PreviewString(preview)+"deleted api dns records:", name, *domain.DomainName)
					}
				}
			}
			if !preview {
				var unexpectedErr error
				err := Retry(ctx, func() error {
					_, err := ApiClient().DeleteDomainName(ctx, &apigatewayv2.DeleteDomainNameInput{
						DomainName: domain.DomainName,
					})
					if err != nil {
						if strings.Contains(err.Error(), "TooManyRequestsException") {
							Logger.Println("delete domain has low rate limits, sleeping then retrying")
							time.Sleep(15 * time.Second)
						} else {
							unexpectedErr = err
							return nil
						}
					}
					return err
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				if unexpectedErr != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Println(PreviewString(preview)+"deleted api domain:", name, *domain.DomainName)
		}
	}
	return nil
}

func lambdaScheduleName(name, schedule string) string {
	return name + lambdaEventRuleNameSeparator + strings.ReplaceAll(base64.StdEncoding.EncodeToString([]byte(schedule)), "=", "")
}

func LambdaEnsureTriggerSchedule(ctx context.Context, infraLambda *InfraLambda, preview bool) ([]string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaEnsureTriggerSchedule"}
		defer d.Log()
	}
	var permissionSids []string
	var triggers []string
	for _, trigger := range infraLambda.Trigger {
		if trigger.Type == lambdaTriggerSchedule {
			triggers = append(triggers, trigger.Attr[0])
		}
	}
	if len(triggers) > 0 {
		for _, schedule := range triggers {
			var scheduleArn string
			scheduleName := lambdaScheduleName(infraLambda.Name, schedule)
			out, err := EventsClient().DescribeRule(ctx, &eventbridge.DescribeRuleInput{
				Name: aws.String(scheduleName),
			})
			if err != nil {
				var rnfe *eventbridgetypes.ResourceNotFoundException
				if !errors.As(err, &rnfe) {
					return nil, err
				}
				if !preview {
					out, err := EventsClient().PutRule(ctx, &eventbridge.PutRuleInput{
						Name:               aws.String(scheduleName),
						ScheduleExpression: aws.String(schedule),
						Tags: []eventbridgetypes.Tag{{
							Key:   aws.String(infraSetTagName),
							Value: aws.String(infraLambda.infraSetName),
						}},
					})
					if err != nil {
						Logger.Println("error:", err)
						return nil, err
					}
					scheduleArn = *out.RuleArn
				}
				Logger.Println(PreviewString(preview)+"created cloudwatch rule:", scheduleName, schedule)
			} else {
				if *out.ScheduleExpression != schedule {
					err := fmt.Errorf("cloudwatch rule misconfigured: %s %s != %s", scheduleName, schedule, *out.ScheduleExpression)
					Logger.Println("error:", err)
					return nil, err
				}
				scheduleArn = *out.Arn
			}
			sid, err := lambdaEnsurePermission(ctx, infraLambda.Name, "events.amazonaws.com", scheduleArn, preview)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			permissionSids = append(permissionSids, sid)
			var targets []eventbridgetypes.Target
			err = Retry(ctx, func() error {
				var err error
				targets, err = EventsListRuleTargets(ctx, scheduleName, nil)
				var rnfe *eventbridgetypes.ResourceNotFoundException
				if errors.As(err, &rnfe) {
					return nil
				}
				return err
			})
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			switch len(targets) {
			case 0:
				if !preview {
					_, err := EventsClient().PutTargets(ctx, &eventbridge.PutTargetsInput{
						Rule: aws.String(scheduleName),
						Targets: []eventbridgetypes.Target{{
							Id:  aws.String("1"),
							Arn: aws.String(infraLambda.Arn),
						}},
					})
					if err != nil {
						Logger.Println("error:", err)
						return nil, err
					}
				}
				Logger.Println(PreviewString(preview)+"created cloudwatch rule target:", scheduleName, infraLambda.Arn)
			case 1:
				if *targets[0].Arn != infraLambda.Arn {
					err := fmt.Errorf("cloudwatch rule is misconfigured with unknown target: %s != %s", infraLambda.Arn, *targets[0].Arn)
					Logger.Println("error:", err)
					return nil, err
				}
			default:
				var targetArns []string
				for _, target := range targets {
					targetArns = append(targetArns, *target.Arn)
				}
				err := fmt.Errorf("cloudwatch rule is misconfigured with unknown targets: %s %v", infraLambda.Arn, targetArns)
				Logger.Println("error:", err)
				return nil, err
			}
		}
	}
	rules, err := EventsListRules(ctx, nil)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	for _, rule := range rules {
		targets, err := EventsListRuleTargets(ctx, *rule.Name, nil)
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		for _, target := range targets {
			if *target.Arn == infraLambda.Arn && rule.ScheduleExpression != nil && !Contains(triggers, *rule.ScheduleExpression) {
				if !preview {
					ids := []string{}
					for _, target := range targets {
						if target.Id != nil {
							ids = append(ids, *target.Id)
						}
					}
					_, err := EventsClient().RemoveTargets(ctx, &eventbridge.RemoveTargetsInput{
						Rule: rule.Name,
						Ids:  ids,
					})
					if err != nil {
						Logger.Println("error:", err)
						return nil, err
					}
					_, err = EventsClient().DeleteRule(ctx, &eventbridge.DeleteRuleInput{
						Name: rule.Name,
					})
					if err != nil {
						Logger.Println("error:", err)
						return nil, err
					}
				}
				Logger.Println(PreviewString(preview)+"deleted schedule trigger:", infraLambda.Name)
				break
			}
		}
	}
	return permissionSids, nil
}

func lambdaDynamoDBTriggerAttrShortcut(s string) string {
	s2, ok := map[string]string{
		"batch":    "BatchSize",
		"parallel": "ParallelizationFactor",
		"retry":    "MaximumRetryAttempts",
		"start":    "StartingPosition",
		"window":   "MaximumBatchingWindowInSeconds",
	}[s]
	if ok {
		return s2
	}
	return s
}

func LambdaEnsureTriggerDynamoDB(ctx context.Context, infraLambda *InfraLambda, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaEnsureTriggerDynamoDB"}
		defer d.Log()
	}
	var triggers [][]string
	var triggerTables []string
	for _, trigger := range infraLambda.Trigger {
		if trigger.Type == lambdaTriggerDynamoDB {
			triggers = append(triggers, trigger.Attr)
			triggerTables = append(triggerTables, trigger.Attr[0])
		}
	}
	if len(triggers) > 0 {
		for _, triggerAttrs := range triggers {
			tableName := triggerAttrs[0]
			triggerAttrs := triggerAttrs[1:]
			createMappingInput := &lambda.CreateEventSourceMappingInput{
				FunctionName:                   aws.String(infraLambda.Name),
				Enabled:                        aws.Bool(true),
				BatchSize:                      aws.Int32(100),
				MaximumBatchingWindowInSeconds: aws.Int32(0),
				MaximumRetryAttempts:           aws.Int32(-1),
				ParallelizationFactor:          aws.Int32(1),
			}
			for _, line := range triggerAttrs {
				attr, value, err := SplitOnce(line, "=")
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				attr = lambdaDynamoDBTriggerAttrShortcut(attr)
				switch attr {
				case "BatchSize":
					size, err := strconv.Atoi(value)
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
					createMappingInput.BatchSize = aws.Int32(int32(size))
				case "MaximumBatchingWindowInSeconds":
					size, err := strconv.Atoi(value)
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
					createMappingInput.MaximumBatchingWindowInSeconds = aws.Int32(int32(size))
				case "MaximumRetryAttempts":
					attempts, err := strconv.Atoi(value)
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
					createMappingInput.MaximumRetryAttempts = aws.Int32(int32(attempts))
				case "ParallelizationFactor":
					factor, err := strconv.Atoi(value)
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
					createMappingInput.ParallelizationFactor = aws.Int32(int32(factor))
				case "StartingPosition":
					createMappingInput.StartingPosition = lambdatypes.EventSourcePosition(strings.ToUpper(value))
				default:
					err := fmt.Errorf("unknown lambda dynamodb trigger attribute: %s", line)
					Logger.Println("error:", err)
					return err
				}
			}
			var found *lambdatypes.EventSourceMappingConfiguration
			count := 0
			streamArn, err := DynamoDBStreamArn(ctx, tableName)
			if err != nil {
				if !strings.Contains(err.Error(), "Requested resource not found") {
					Logger.Println("error:", err)
					return err
				}
			} else {
				createMappingInput.EventSourceArn = aws.String(streamArn)
				eventSourceMappings, err := lambdaListEventSourceMappings(ctx, infraLambda.Name)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				for _, mapping := range eventSourceMappings {
					if *mapping.EventSourceArn == streamArn && *mapping.FunctionArn == infraLambda.Arn {
						found = &mapping
						count++
					}
				}
			}
			switch count {
			case 0:
				if !preview {
					err := Retry(ctx, func() error {
						_, err := LambdaClient().CreateEventSourceMapping(ctx, createMappingInput)
						return err
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
				}
				Logger.Println(PreviewString(preview)+"created event source mapping:", infraLambda.Name, infraLambda.Arn, streamArn, strings.Join(triggerAttrs, " "))
			case 1:
				needsUpdate := false
				update := &lambda.UpdateEventSourceMappingInput{UUID: found.UUID}
				update.FunctionName = createMappingInput.FunctionName
				if *found.BatchSize != *createMappingInput.BatchSize {
					Logger.Printf(PreviewString(preview)+"will update lambda event source mapping BatchSize for %s %s: %d => %d\n", infraLambda.Name, tableName, *found.BatchSize, *createMappingInput.BatchSize)
					update.BatchSize = createMappingInput.BatchSize
					needsUpdate = true
				}
				if *found.MaximumRetryAttempts != *createMappingInput.MaximumRetryAttempts {
					Logger.Printf(PreviewString(preview)+"will update lambda event source mapping MaximumRetryAttempts for %s %s: %d => %d\n", infraLambda.Name, tableName, *found.MaximumRetryAttempts, *createMappingInput.MaximumRetryAttempts)
					update.MaximumRetryAttempts = createMappingInput.MaximumRetryAttempts
					needsUpdate = true
				}
				if *found.ParallelizationFactor != *createMappingInput.ParallelizationFactor {
					Logger.Printf(PreviewString(preview)+"will update lambda event source mapping ParallelizationFactor for %s %s: %d => %d\n", infraLambda.Name, tableName, *found.ParallelizationFactor, *createMappingInput.ParallelizationFactor)
					update.ParallelizationFactor = createMappingInput.ParallelizationFactor
					needsUpdate = true
				}
				if *found.MaximumBatchingWindowInSeconds != *createMappingInput.MaximumBatchingWindowInSeconds {
					Logger.Printf(PreviewString(preview)+"will update lambda event source mapping MaximumBatchingWindowInSeconds for %s %s: %d => %d\n", infraLambda.Name, tableName, *found.MaximumBatchingWindowInSeconds, *createMappingInput.MaximumBatchingWindowInSeconds)
					update.MaximumBatchingWindowInSeconds = createMappingInput.MaximumBatchingWindowInSeconds
					needsUpdate = true
				}
				if found.StartingPosition != createMappingInput.StartingPosition {
					err := fmt.Errorf("cannot update StartingPosition for %s %s: %s => %s", infraLambda.Name, tableName, found.StartingPosition, createMappingInput.StartingPosition)
					Logger.Println("error:", err)
					return err
				}
				if needsUpdate {
					if !preview {
						_, err := LambdaClient().UpdateEventSourceMapping(ctx, update)
						if err != nil {
							Logger.Println("error:", err)
							return err
						}
					}
					Logger.Println(PreviewString(preview)+"updated event source mapping for", infraLambda.Name, tableName)
				}
			default:
				err := fmt.Errorf("found more than 1 event source mapping for %s %s", infraLambda.Name, tableName)
				Logger.Println("error:", err)
				return err
			}
		}
	}
	var marker *string
	for {
		out, err := LambdaClient().ListEventSourceMappings(ctx, &lambda.ListEventSourceMappingsInput{
			FunctionName: aws.String(infraLambda.Arn),
			Marker:       marker,
		})
		if err != nil {
			if !preview {
				Logger.Println("error:", err)
				return err
			}
			out = &lambda.ListEventSourceMappingsOutput{}
		}
		for _, mapping := range out.EventSourceMappings {
			infra := ArnToInfraName(*mapping.EventSourceArn)
			if infra != lambdaTriggerDynamoDB {
				continue
			}
			tableName := DynamoDBStreamArnToTableName(*mapping.EventSourceArn)
			if !Contains(triggerTables, tableName) {
				if !preview {
					_, err := LambdaClient().DeleteEventSourceMapping(ctx, &lambda.DeleteEventSourceMappingInput{
						UUID: mapping.UUID,
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
				}
				Logger.Println(PreviewString(preview)+"deleted trigger:", infraLambda.Name, tableName)
			}
		}
		if out.NextMarker == nil {
			break
		}
		marker = out.NextMarker
	}
	return nil
}

func lambdaListEventSourceMappings(ctx context.Context, name string) ([]lambdatypes.EventSourceMappingConfiguration, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaListEventSourceMappings"}
		defer d.Log()
	}
	var marker *string
	var eventSourceMappings []lambdatypes.EventSourceMappingConfiguration
	for {
		out, err := LambdaClient().ListEventSourceMappings(ctx, &lambda.ListEventSourceMappingsInput{
			FunctionName: aws.String(name),
			Marker:       marker,
		})
		if err != nil {
			return nil, err
		}
		eventSourceMappings = append(eventSourceMappings, out.EventSourceMappings...)
		if out.NextMarker == nil {
			break
		}
		marker = out.NextMarker
	}
	return eventSourceMappings, nil
}

func lambdaSQSTriggerAttrShortcut(s string) string {
	s2, ok := map[string]string{
		"batch":  "BatchSize",
		"window": "MaximumBatchingWindowInSeconds",
	}[s]
	if ok {
		return s2
	}
	return s
}

func LambdaEnsureTriggerSQS(ctx context.Context, infraLambda *InfraLambda, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaEnsureTriggerSQS"}
		defer d.Log()
	}
	var triggers [][]string
	var queueNames []string
	for _, trigger := range infraLambda.Trigger {
		if trigger.Type == lambdaTriggerSQS {
			triggers = append(triggers, trigger.Attr)
			queueNames = append(queueNames, trigger.Attr[0])
		}
	}
	if len(triggers) > 0 {
		for _, triggerAttrs := range triggers {
			queueName := triggerAttrs[0]
			triggerAttrs := triggerAttrs[1:]
			sqsArn, err := SQSArn(ctx, queueName)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			input := &lambda.CreateEventSourceMappingInput{
				FunctionName:                   aws.String(infraLambda.Name),
				EventSourceArn:                 aws.String(sqsArn),
				Enabled:                        aws.Bool(true),
				BatchSize:                      aws.Int32(10),
				MaximumBatchingWindowInSeconds: aws.Int32(0),
			}
			for _, line := range triggerAttrs {
				attr, value, err := SplitOnce(line, "=")
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				attr = lambdaSQSTriggerAttrShortcut(attr)
				switch attr {
				case "BatchSize":
					size, err := strconv.Atoi(value)
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
					input.BatchSize = aws.Int32(int32(size))
				case "MaximumBatchingWindowInSeconds":
					size, err := strconv.Atoi(value)
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
					input.MaximumBatchingWindowInSeconds = aws.Int32(int32(size))
				default:
					err := fmt.Errorf("unknown sqs trigger attribute: %s", line)
					Logger.Println("error:", err)
					return err
				}
			}
			eventSourceMappings, err := lambdaListEventSourceMappings(ctx, infraLambda.Name)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			count := 0
			var found *lambdatypes.EventSourceMappingConfiguration
			for _, mapping := range eventSourceMappings {
				if mapping.EventSourceArn != nil && *mapping.EventSourceArn == sqsArn && mapping.FunctionArn != nil && *mapping.FunctionArn == infraLambda.Arn {
					found = &mapping
					count++
				}
			}
			switch count {
			case 0:
				if !preview {
					err := Retry(ctx, func() error {
						_, err := LambdaClient().CreateEventSourceMapping(ctx, input)
						return err
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
				}
				Logger.Println(PreviewString(preview)+"created event source mapping:", infraLambda.Name, infraLambda.Arn, sqsArn, strings.Join(triggerAttrs, " "))
			case 1:
				needsUpdate := false
				update := &lambda.UpdateEventSourceMappingInput{UUID: found.UUID}
				update.FunctionName = input.FunctionName
				if *found.BatchSize != *input.BatchSize {
					Logger.Printf(PreviewString(preview)+"will update lambda event source mapping BatchSize for %s %s: %d => %d\n", infraLambda.Name, queueName, *found.BatchSize, *input.BatchSize)
					update.BatchSize = input.BatchSize
					needsUpdate = true
				}
				if *found.MaximumBatchingWindowInSeconds != *input.MaximumBatchingWindowInSeconds {
					Logger.Printf(PreviewString(preview)+"will update lambda event source mapping MaximumBatchingWindowInSeconds for %s %s: %d => %d\n", infraLambda.Name, queueName, *found.MaximumBatchingWindowInSeconds, *input.MaximumBatchingWindowInSeconds)
					update.MaximumBatchingWindowInSeconds = input.MaximumBatchingWindowInSeconds
					needsUpdate = true
				}
				if needsUpdate {
					if !preview {
						_, err := LambdaClient().UpdateEventSourceMapping(ctx, update)
						if err != nil {
							Logger.Println("error:", err)
							return err
						}
					}
					Logger.Println(PreviewString(preview)+"updated event source mapping for", infraLambda.Name, queueName)
				}
			default:
				err := fmt.Errorf("found more than 1 event source mapping for %s %s", infraLambda.Name, queueName)
				Logger.Println("error:", err)
				return err
			}
		}
	}
	var marker *string
	for {
		out, err := LambdaClient().ListEventSourceMappings(ctx, &lambda.ListEventSourceMappingsInput{
			FunctionName: aws.String(infraLambda.Arn),
			Marker:       marker,
		})
		if err != nil {
			if !preview {
				Logger.Println("error:", err)
				return err
			}
			out = &lambda.ListEventSourceMappingsOutput{}
		}
		for _, mapping := range out.EventSourceMappings {
			infra := ArnToInfraName(*mapping.EventSourceArn)
			if infra != lambdaTriggerSQS {
				continue
			}
			queueName := SQSArnToName(*mapping.EventSourceArn)
			if !Contains(queueNames, queueName) {
				if !preview {
					err := Retry(ctx, func() error {
						_, err := LambdaClient().DeleteEventSourceMapping(ctx, &lambda.DeleteEventSourceMappingInput{
							UUID: mapping.UUID,
						})
						return err
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
				}
				Logger.Println(PreviewString(preview)+"deleted trigger:", infraLambda.Name, queueName)
			}
		}
		if out.NextMarker == nil {
			break
		}
		marker = out.NextMarker
	}
	return nil
}

func LambdaZipFile(name string) string {
	return fmt.Sprintf("/tmp/%s/lambda.zip", name)
}

func lambdaUpdateZipGo(infraLambda *InfraLambda) error {
	return lambdaCreateZipGo(infraLambda)
}

func lambdaCreateZipGo(infraLambda *InfraLambda) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaCreateZipGo"}
		defer d.Log()
	}
	zipFile := LambdaZipFile(infraLambda.Name)
	dir := path.Dir(zipFile)
	err := os.RemoveAll(dir)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	_ = os.MkdirAll(dir, os.ModePerm)
	err = shellAt(path.Dir(infraLambda.Entrypoint), "CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags='-s -w %s' -tags 'netgo osusergo purego' -o %s %s",
		os.Getenv("LDFLAGS"),
		path.Join(dir, "bootstrap"),
		path.Base(infraLambda.Entrypoint),
	)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	compression := "-9"
	if os.Getenv("ZIP_COMPRESSION") != "" {
		compression = "-" + os.Getenv("ZIP_COMPRESSION")
	}
	err = shellAt(dir, "zip %s %s ./bootstrap", compression, zipFile)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	return nil
}

func lambdaCreateZipPy(infraLambda *InfraLambda) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaCreateZipPy"}
		defer d.Log()
	}
	zipFile := LambdaZipFile(infraLambda.Name)
	dir := path.Dir(zipFile)
	err := os.RemoveAll(dir)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	_ = os.MkdirAll(dir, os.ModePerm)
	err = shell("virtualenv --python python3 %s/env", dir)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if len(infraLambda.Require) > 0 {
		var args []string
		for _, require := range infraLambda.Require {
			args = append(args, fmt.Sprintf(`"%s"`, require))
		}
		arg := strings.Join(args, " ")
		err = shell("%s/env/bin/pip install %s", dir, arg)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	site_packages, err := filepath.Glob(fmt.Sprintf("%s/env/lib/python3*/site-packages", dir))
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if len(site_packages) != 1 {
		err := fmt.Errorf("expected 1 site-package dir: %v", site_packages)
		Logger.Println("error:", err)
		return err
	}
	site_package := site_packages[0]
	err = shellAt(site_package, "cp %s .", infraLambda.Entrypoint)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = shellAt(site_package, "rm -rf wheel pip setuptools pkg_resources easy_install.py")
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = shellAt(site_package, "ls | grep -E 'info$' | grep -v ' ' | xargs rm -rf")
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	compression := "-9"
	if os.Getenv("ZIP_COMPRESSION") != "" {
		compression = "-" + os.Getenv("ZIP_COMPRESSION")
	}
	err = shellAt(site_package, "zip %s -r %s .", compression, zipFile)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	return nil
}

func LambdaZipBytes(infraLambda *InfraLambda) ([]byte, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaZipBytes"}
		defer d.Log()
	}
	zipFile := LambdaZipFile(infraLambda.Name)
	data, err := os.ReadFile(zipFile)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	return data, nil
}

func LambdaIncludeInZip(infraLambda *InfraLambda) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaIncludeInZip"}
		defer d.Log()
	}
	zipFile := LambdaZipFile(infraLambda.Name)
	dir := infraLambda.dir
	var includes []string
	for _, include := range infraLambda.Include {
		if !strings.Contains(include, "*") {
			includes = append(includes, include)
		} else {
			paths, err := filepath.Glob(path.Join(dir, include))
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			for _, pth := range paths {
				pth, err := filepath.Rel(dir, pth)
				if err != nil {
					panic(err)
				}
				includes = append(includes, pth)
			}
		}
	}
	for _, include := range includes {
		_, errLink := os.Readlink(include)
		if !Exists(include) && errLink != nil {
			err := fmt.Errorf("no such path for include: %s", include)
			Logger.Println("error:", err)
			return err
		}
		args := ""
		if strings.HasPrefix(include, "/") {
			args = "--junk-paths"
		}
		compression := "-9"
		if os.Getenv("ZIP_COMPRESSION") != "" {
			compression = "-" + os.Getenv("ZIP_COMPRESSION")
		}
		err := shellAt(dir, "zip %s %s --symlinks -r %s '%s'", compression, args, zipFile, include)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	return nil
}

func lambdaUpdateZipPy(infraLambda *InfraLambda) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaUpdateZipPy"}
		defer d.Log()
	}
	zipFile := LambdaZipFile(infraLambda.Name)
	dir := path.Dir(zipFile)
	site_packages, err := filepath.Glob(fmt.Sprintf("%s/env/lib/python3*/site-packages", dir))
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if len(site_packages) != 1 {
		err := fmt.Errorf("expected 1 site-package dir: %v", site_packages)
		Logger.Println("error:", err)
		return err
	}
	site_package := site_packages[0]
	err = shellAt(site_package, "cp %s .", infraLambda.Entrypoint)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	compression := "-9"
	if os.Getenv("ZIP_COMPRESSION") != "" {
		compression = "-" + os.Getenv("ZIP_COMPRESSION")
	}
	err = shellAt(site_package, "zip %s %s %s", compression, zipFile, path.Base(infraLambda.Entrypoint))
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	Logger.Println("updated zip:", zipFile, infraLambda.Entrypoint)
	return nil
}

func LambdaListFunctions(ctx context.Context) ([]lambdatypes.FunctionConfiguration, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaListFunctions"}
		defer d.Log()
	}
	var marker *string
	var functions []lambdatypes.FunctionConfiguration
	for {
		out, err := LambdaClient().ListFunctions(ctx, &lambda.ListFunctionsInput{
			Marker: marker,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		functions = append(functions, out.Functions...)
		if out.NextMarker == nil {
			break
		}
		marker = out.NextMarker
	}
	return functions, nil
}

type LambdaUpdateZipFn func(infraLambda *InfraLambda) error

type LambdaCreateZipFn func(infraLambda *InfraLambda) error

func lambdaEnsure(ctx context.Context, infraLambda *InfraLambda, quick, preview, showEnvVarValues bool, updateZipFn LambdaUpdateZipFn, createZipFn LambdaCreateZipFn) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "lambdaEnsure"}
		defer d.Log()
	}
	var err error
	concurrency := lambdaAttrConcurrencyDefault
	memory := lambdaAttrMemoryDefault
	timeout := lambdaAttrTimeoutDefault
	logsTTLDays := lambdaAttrLogsTTLDaysDefault
	for _, attr := range infraLambda.Attr {
		k, v, err := SplitOnce(attr, "=")
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		switch k {
		case lambdaAttrConcurrency:
			concurrency = Atoi(v)
		case lambdaAttrMemory:
			memory = Atoi(v)
		case lambdaAttrTimeout:
			timeout = Atoi(v)
		case lambdaAttrLogsTTLDays:
			logsTTLDays = Atoi(v)
		default:
			err := fmt.Errorf("unknown attr: %s", k)
			Logger.Println("error:", err)
			return err
		}
	}
	zipFile := LambdaZipFile(infraLambda.Name)
	if quick && !(infraLambda.runtime == lambdaRuntimePython && !Exists(zipFile)) { // python requires existing zip for quick, since it only adds source instead of rebuilding the virtualenv, which is way faster
		err = updateZipFn(infraLambda)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = LambdaIncludeInZip(infraLambda)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = LambdaUpdateFunctionCode(ctx, infraLambda, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		return nil
	}
	err = LogsEnsureGroup(ctx, infraLambda.infraSetName, "/aws/lambda/"+infraLambda.Name, logsTTLDays, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = IamEnsureRole(ctx, infraLambda.infraSetName, infraLambda.Name, "lambda", preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = IamEnsureRolePolicies(ctx, infraLambda.Name, infraLambda.Policy, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	arnRole, err := IamRoleArn(ctx, "lambda", infraLambda.Name)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	var zipBytes []byte
	if infraLambda.runtime != lambdaRuntimeContainer {
		err = createZipFn(infraLambda)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = LambdaIncludeInZip(infraLambda)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		zipBytes, err = LambdaZipBytes(infraLambda)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	var getFunctionOut *lambda.GetFunctionOutput
	var expectedErr error
	err = Retry(ctx, func() error {
		out, err := LambdaClient().GetFunction(ctx, &lambda.GetFunctionInput{
			FunctionName: aws.String(infraLambda.Name),
		})
		if err != nil {
			var notFound *lambdatypes.ResourceNotFoundException
			if errors.As(err, &notFound) {
				expectedErr = err
				return nil
			}
			return err
		}
		getFunctionOut = out
		return nil
	})
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if getFunctionOut == nil {
		getFunctionOut = &lambda.GetFunctionOutput{}
	}
	createInput := &lambda.CreateFunctionInput{
		FunctionName: aws.String(infraLambda.Name),
		Timeout:      aws.Int32(int32(timeout)),
		MemorySize:   aws.Int32(int32(memory)),
		Role:         aws.String(arnRole),
		Code:         &lambdatypes.FunctionCode{},
		Environment:  &lambdatypes.Environment{Variables: map[string]string{}},
		Tags:         map[string]string{infraSetTagName: infraLambda.infraSetName},
	}
	for _, val := range infraLambda.Env {
		k, v, err := SplitOnce(val, "=")
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		if len(regexp.MustCompile(`[^a-zA-Z0-9_]`).FindAllString(k, -1)) > 0 {
			err := fmt.Errorf("env vars must be named '[a-zA-Z0-9_]+', got: %s", k)
			Logger.Println("error:", err)
			return err
		}
		createInput.Environment.Variables[k] = v
	}
	if infraLambda.runtime == lambdaRuntimeContainer {
		createInput.Code.ImageUri = aws.String(infraLambda.Entrypoint)
		createInput.PackageType = lambdatypes.PackageTypeImage
	} else {
		createInput.Code.ZipFile = zipBytes
		createInput.PackageType = lambdatypes.PackageTypeZip
		createInput.Runtime = lambdatypes.Runtime(infraLambda.runtime)
		createInput.Handler = aws.String(infraLambda.handler)
	}
	if expectedErr != nil { // create lambda
		if infraLambda.runtime == lambdaRuntimeContainer {
			existing := map[string]string{}
			new := map[string]string{"entrypoint": *createInput.Code.ImageUri}
			_, err := diffMapStringString(new, existing, PreviewString(preview)+"container", true)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		} else {
			existing := map[string]string{}
			new, err := zipSha256Hex(zipBytes)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			_, err = diffMapStringString(new, existing, PreviewString(preview)+"zip", true)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		if !preview {
			err := Retry(ctx, func() error {
				out, err := LambdaClient().CreateFunction(ctx, createInput)
				if err != nil {
					return err
				}
				getFunctionOut = &lambda.GetFunctionOutput{
					Configuration: &lambdatypes.FunctionConfiguration{
						FunctionArn: out.FunctionArn,
					},
					Code: &lambdatypes.FunctionCodeLocation{
						ImageUri: createInput.Code.ImageUri,
					},
				}
				return nil
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		logPrefix := PreviewString(preview) + "updated env var for: " + infraLambda.Name + ","
		_, err := diffMapStringString(createInput.Environment.Variables, map[string]string{}, logPrefix, showEnvVarValues)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		Logger.Printf(PreviewString(preview)+"update timeout: %d => %d\n", 0, timeout)
		Logger.Printf(PreviewString(preview)+"update memory: %d => %d\n", 0, memory)
		Logger.Println(PreviewString(preview) + "created function: " + infraLambda.Name)
	} else { // update lambda
		var diff bool
		if infraLambda.runtime == lambdaRuntimeContainer {
			existing := map[string]string{"entrypoint": *getFunctionOut.Code.ImageUri}
			new := map[string]string{"entrypoint": *createInput.Code.ImageUri}
			diff, err = diffMapStringString(new, existing, PreviewString(preview)+"entrypoint", true)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		} else {
			httpOut, err := http.Get(*getFunctionOut.Code.Location)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			defer func() { _ = httpOut.Body.Close() }()
			data, err := io.ReadAll(httpOut.Body)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			existing, err := zipSha256Hex(data)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			new, err := zipSha256Hex(zipBytes)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			diff, err = diffMapStringString(new, existing, PreviewString(preview)+"zip", true)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		if diff {
			err := LambdaUpdateFunctionCode(ctx, infraLambda, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		outConf, err := LambdaClient().GetFunctionConfiguration(ctx, &lambda.GetFunctionConfigurationInput{
			FunctionName: aws.String(infraLambda.Name),
		})
		if err != nil {
			if !preview {
				Logger.Println("error:", err)
				return err
			}
			outConf = &lambda.GetFunctionConfigurationOutput{}
		}
		if outConf.Environment == nil {
			outConf.Environment = &lambdatypes.EnvironmentResponse{
				Variables: map[string]string{},
			}
		}
		needsUpdate := false
		logPrefix := PreviewString(preview) + "updated env var for: " + infraLambda.Name + ","
		diff, err = diffMapStringString(
			createInput.Environment.Variables,
			outConf.Environment.Variables,
			logPrefix,
			showEnvVarValues,
		)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		if diff {
			needsUpdate = true
		}
		if outConf.Timeout == nil {
			outConf.Timeout = aws.Int32(0)
		}
		if *outConf.Timeout != int32(timeout) {
			needsUpdate = true
			Logger.Printf(PreviewString(preview)+"update timeout: %d => %d\n", *outConf.Timeout, timeout)
		}
		if outConf.MemorySize == nil {
			outConf.MemorySize = aws.Int32(0)
		}
		if *outConf.MemorySize != int32(memory) {
			needsUpdate = true
			Logger.Printf(PreviewString(preview)+"update memory: %d => %d\n", *outConf.MemorySize, memory)
		}
		if needsUpdate {
			if !preview {
				err := Retry(ctx, func() error {
					_, err := LambdaClient().UpdateFunctionConfiguration(ctx, &lambda.UpdateFunctionConfigurationInput{
						FunctionName: aws.String(infraLambda.Name),
						Timeout:      aws.Int32(int32(timeout)),
						MemorySize:   aws.Int32(int32(memory)),
						Environment: &lambdatypes.Environment{
							Variables: createInput.Environment.Variables,
						},
					})
					return err
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Println(PreviewString(preview)+"updated function configuration:", infraLambda.Name)
		}
	}
	if getFunctionOut.Configuration != nil {
		infraLambda.Arn = *getFunctionOut.Configuration.FunctionArn
	}
	err = LambdaEnsureTriggerDynamoDB(ctx, infraLambda, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	sids, err := LambdaEnsureTriggerSchedule(ctx, infraLambda, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	var permissionSids []string
	permissionSids = append(permissionSids, sids...)
	sid, err := LambdaEnsureTriggerSes(ctx, infraLambda, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	permissionSids = append(permissionSids, sid)
	sids, err = LambdaEnsureTriggerEcr(ctx, infraLambda, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	permissionSids = append(permissionSids, sids...)
	sids, err = LambdaEnsureTriggerApi(ctx, infraLambda, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	// ensure role allows after api trigger because it defines $API_ID and WEBSOCKET_ID
	err = IamEnsureRoleAllows(ctx, infraLambda.Name, infraLambda.Allow, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	permissionSids = append(permissionSids, sids...)
	sids, err = LambdaEnsureTriggerS3(ctx, infraLambda, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	permissionSids = append(permissionSids, sids...)
	err = LambdaEnsureTriggerSQS(ctx, infraLambda, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = LambdaSetConcurrency(ctx, infraLambda.Name, concurrency, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = lambdaRemoveUnusedPermissions(ctx, infraLambda.Name, permissionSids, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	return nil
}

func LambdaUpdateFunctionCode(ctx context.Context, infraLambda *InfraLambda, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaUpdateFunctionCode"}
		defer d.Log()
	}
	if !preview {
		var expectedErr error
		err := Retry(ctx, func() error {
			updateInput := &lambda.UpdateFunctionCodeInput{
				FunctionName: aws.String(infraLambda.Name),
			}
			if infraLambda.runtime == lambdaRuntimeContainer {
				updateInput.ImageUri = aws.String(infraLambda.Entrypoint)
			} else {
				zipBytes, err := LambdaZipBytes(infraLambda)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				updateInput.ZipFile = zipBytes
			}
			_, err := LambdaClient().UpdateFunctionCode(ctx, updateInput)
			if err != nil {
				var notFound *lambdatypes.ResourceNotFoundException
				var tooLarge *lambdatypes.InvalidParameterValueException
				if errors.As(err, &notFound) || errors.As(err, &tooLarge) {
					expectedErr = err
					return nil
				}
				return err
			}
			return nil
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		if expectedErr != nil {
			Logger.Println("error:", expectedErr)
			return expectedErr
		}
	}
	Logger.Println(PreviewString(preview) + "lambda updated code for: " + infraLambda.Name)
	return nil
}

func LambdaDeleteFunction(ctx context.Context, name string, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaDeleteFunction"}
		defer d.Log()
	}
	_, err := LambdaClient().GetFunction(ctx, &lambda.GetFunctionInput{
		FunctionName: aws.String(name),
	})
	if err != nil {
		var notFound *lambdatypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return nil
		}
	}
	if !preview {
		err := Retry(ctx, func() error {
			_, err := LambdaClient().DeleteFunction(ctx, &lambda.DeleteFunctionInput{
				FunctionName: aws.String(name),
			})
			if err != nil {
				var notFound *lambdatypes.ResourceNotFoundException
				if errors.As(err, &notFound) {
					return nil
				}
				return err
			}
			return nil
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	Logger.Println(PreviewString(preview)+"deleted function:", name)
	return nil
}

func LambdaDelete(ctx context.Context, name string, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LambdaDelete"}
		defer d.Log()
	}
	triggerChan := make(chan *InfraTrigger)
	close(triggerChan)
	infraLambdas, err := InfraListLambda(ctx, triggerChan, name)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	for lambdaName, infraLambda := range infraLambdas {
		if lambdaName != name {
			continue
		}
		infraLambda.Name = lambdaName
		infraLambda.Arn, _ = LambdaArn(ctx, lambdaName)
		infraLambda.Trigger = nil
		_, err := LambdaEnsureTriggerApi(ctx, infraLambda, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		_, err = LambdaEnsureTriggerSes(ctx, infraLambda, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		if infraLambda.Arn != "" {
			_, err := LambdaEnsureTriggerS3(ctx, infraLambda, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			_, err = LambdaEnsureTriggerEcr(ctx, infraLambda, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			_, err = LambdaEnsureTriggerSchedule(ctx, infraLambda, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			err = LambdaEnsureTriggerDynamoDB(ctx, infraLambda, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			err = LambdaEnsureTriggerSQS(ctx, infraLambda, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		err = IamDeleteRole(ctx, lambdaName, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = LambdaDeleteFunction(ctx, lambdaName, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = LogsDeleteGroup(ctx, "/aws/lambda/"+lambdaName, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	return nil
}
