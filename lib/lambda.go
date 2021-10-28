package lib

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"

	"strings"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/apigateway"
	"github.com/aws/aws-sdk-go/service/cloudwatchevents"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/aws/aws-sdk-go/service/s3"
)

const (
	//
	lambdaAttrConcurrency = "concurrency"
	lambdaAttrMemory      = "memory"
	lambdaAttrTimeout     = "timeout"
	//
	lambdaAttrConcurrencyDefault = 0
	lambdaAttrMemoryDefault      = 128
	lambdaAttrTimeoutDefault     = 300
	//
	lambdaTriggerSQS        = "sqs"
	lambdaTrigerS3          = "s3"
	lambdaTriggerDynamoDB   = "dynamodb"
	lambdaTriggerApi        = "api"
	lambdaTriggerCloudwatch = "cloudwatch"
	//
	lambdaMetaS3       = "s3"
	lambdaMetaDynamoDB = "dynamodb"
	lambdaMetaSQS      = "sqs"
	lambdaMetaPolicy   = "policy"
	lambdaMetaAllow    = "allow"
	lambdaMetaInclude  = "include"
	lambdaMetaTrigger  = "trigger"
	lambdaMetaRequire  = "require"
	lambdaMetaAttr     = "attr"
)

var lambdaClient *lambda.Lambda
var lambdaClientLock sync.RWMutex

func LambdaClient() *lambda.Lambda {
	lambdaClientLock.Lock()
	defer lambdaClientLock.Unlock()
	if lambdaClient == nil {
		lambdaClient = lambda.New(Session())
	}
	return lambdaClient
}

func LambdaSetConcurrency(ctx context.Context, name string, concurrency int, preview bool) error {
	if concurrency > 0 {
		out, err := LambdaClient().GetFunctionConcurrencyWithContext(ctx, &lambda.GetFunctionConcurrencyInput{
			FunctionName: aws.String(name),
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		if int(*out.ReservedConcurrentExecutions) != concurrency {
			if !preview {
				_, err := LambdaClient().PutFunctionConcurrencyWithContext(ctx, &lambda.PutFunctionConcurrencyInput{
					FunctionName:                 aws.String(name),
					ReservedConcurrentExecutions: aws.Int64(int64(concurrency)),
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Println(PreviewString(preview)+"updated concurrency:", name, concurrency)
		}
	}
	return nil
}

func LambdaName(pth string) (string, error) {
	if !Exists(pth) {
		return pth, nil
	}
	name := path.Base(pth)
	if strings.Count(name, ".") != 1 {
		err := fmt.Errorf("path should not include '.' except for one file extension")
		Logger.Println("error:", err)
		return "", err
	}
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.Split(name, ".")[0]
	return name, nil
}

func LambdaArn(ctx context.Context, name string) (string, error) {
	var expectedErr error
	var arn string
	err := Retry(ctx, func() error {
		out, err := LambdaClient().GetFunction(&lambda.GetFunctionInput{
			FunctionName: aws.String(name),
		})
		if err != nil {
			aerr, ok := err.(awserr.Error)
			if ok && aerr.Code() == lambda.ErrCodeResourceNotFoundException {
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
		Logger.Println("error:", err)
		return "", expectedErr
	}
	return arn, nil
}

func lambdaFilterMetadata(lines []string) []string {
	var res []string
	for _, line := range lines {
		line = strings.Trim(line, "\n")
		if (strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//")) && strings.Contains(line, ":") {
			res = append(res, strings.Trim(line, "#/ "))
		}
		parts := strings.Split(line, " ")
		if len(parts) > 0 && Contains([]string{"import", "def", "func"}, parts[0]) {
			break
		}
	}
	return res
}

func lambdaParseMetadata(token string, lines []string, silent bool) ([]string, error) {
	token = token + ":"
	var vals [][]string
	for _, line := range lambdaFilterMetadata(lines) {
		if strings.HasPrefix(line, token) {
			part := Last(strings.SplitN(line, token, 2))
			part = strings.Split(part, "#")[0]
			part = strings.Split(part, "//")[0]
			part = strings.Trim(part, " ")
			part = regexp.MustCompile(` +`).ReplaceAllString(part, " ")
			vals = append(vals, []string{line, part})
		}
	}
	var results []string
	for _, val := range vals {
		line := val[0]
		part := val[1]
		for _, variable := range regexp.MustCompile(`(\$\{[^\}]+})`).FindAllString(part, -1) {
			variableName := variable[2 : len(variable)-1]
			variableValue := os.Getenv(variableName)
			if variableValue == "" {
				err := fmt.Errorf("missing environment variable: %s", line)
				Logger.Println("error:", err)
				return nil, err
			}
			part = strings.Replace(part, variable, variableValue, 1)
		}
		results = append(results, part)
	}
	return results, nil
}

type LambdaMetadata struct {
	S3       []string
	DynamoDB []string
	Sqs      []string
	Policy   []string
	Allow    []string
	Include  []string
	Trigger  []string
	Require  []string
	Attr     []string
}

func LambdaGetMetadata(lines []string, silent bool) (*LambdaMetadata, error) {
	var err error
	meta := &LambdaMetadata{}
	meta.S3, err = lambdaParseMetadata(lambdaMetaS3, lines, silent)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.DynamoDB, err = lambdaParseMetadata(lambdaMetaDynamoDB, lines, silent)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Sqs, err = lambdaParseMetadata(lambdaMetaSQS, lines, silent)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Policy, err = lambdaParseMetadata(lambdaMetaPolicy, lines, silent)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Allow, err = lambdaParseMetadata(lambdaMetaAllow, lines, silent)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Include, err = lambdaParseMetadata(lambdaMetaInclude, lines, silent)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Trigger, err = lambdaParseMetadata(lambdaMetaTrigger, lines, silent)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Require, err = lambdaParseMetadata(lambdaMetaRequire, lines, silent)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Attr, err = lambdaParseMetadata(lambdaMetaAttr, lines, silent)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	for _, conf := range meta.Attr {
		parts := strings.SplitN(conf, " ", 2)
		k := parts[0]
		v := parts[1]
		if !Contains([]string{lambdaAttrConcurrency, lambdaAttrMemory, lambdaAttrTimeout}, k) {
			err := fmt.Errorf("unknown attr: %s", k)
			Logger.Println("error:", err)
			return nil, err
		}
		if !IsDigit(v) {
			err := fmt.Errorf("conf value should be digits: %s %s", k, v)
			Logger.Println("error:", err)
			return nil, err
		}
	}
	for _, trigger := range meta.Trigger {
		if !Contains([]string{lambdaTriggerSQS, lambdaTrigerS3, lambdaTriggerDynamoDB, lambdaTriggerApi, lambdaTriggerCloudwatch}, strings.Split(trigger, " ")[0]) {
			err := fmt.Errorf("unknown trigger: %s", trigger)
			Logger.Println("error:", err)
			return nil, err
		}
	}
	for _, line := range lambdaFilterMetadata(lines) {
		token := strings.SplitN(line, ":", 2)[0]
		if !Contains([]string{lambdaMetaS3, lambdaMetaDynamoDB, lambdaMetaSQS, lambdaMetaPolicy, lambdaMetaAllow, lambdaMetaInclude, lambdaMetaTrigger, lambdaMetaRequire, lambdaMetaAttr}, token) {
			err := fmt.Errorf("unknown configuration comment: %s", line)
			Logger.Println("error:", err)
			return nil, err
		}
	}
	return meta, nil
}

func LambdaEnsureTriggerS3(ctx context.Context, name, arnLambda string, meta *LambdaMetadata, preview bool) error {
	events := []*string{aws.String("s3:ObjectCreated:*"), aws.String("s3:ObjectRemoved:*")}
	var triggers []string
	for _, trigger := range meta.Trigger {
		parts := strings.Split(trigger, " ")
		kind := parts[0]
		if kind == lambdaTrigerS3 {
			bucket := parts[1]
			triggers = append(triggers, bucket)
		}
	}
	if len(triggers) > 0 {
		for _, bucket := range triggers {
			err := lambdaEnsurePermission(ctx, name, "s3.amazonaws.com", "arn:aws:s3:::"+bucket, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			s3Client, err := S3ClientBucketRegion(bucket)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			out, err := s3Client.GetBucketNotificationConfigurationWithContext(ctx, &s3.GetBucketNotificationConfigurationRequest{
				Bucket: aws.String(bucket),
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			var existingEvents []*string
			for _, conf := range out.LambdaFunctionConfigurations {
				if *conf.LambdaFunctionArn == arnLambda {
					existingEvents = conf.Events
				}
			}
			if !reflect.DeepEqual(existingEvents, events) {
				var confs []*s3.LambdaFunctionConfiguration
				for _, conf := range out.LambdaFunctionConfigurations {
					if *conf.LambdaFunctionArn == arnLambda {
						confs = append(confs, &s3.LambdaFunctionConfiguration{
							LambdaFunctionArn: aws.String(arnLambda),
							Events:            events,
						})
					} else {
						confs = append(confs, conf)
					}
				}
				out.LambdaFunctionConfigurations = confs
				if !preview {
					_, err := s3Client.PutBucketNotificationConfigurationWithContext(ctx, &s3.PutBucketNotificationConfigurationInput{
						Bucket:                    aws.String(bucket),
						NotificationConfiguration: out,
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
				}
				Logger.Printf(PreviewString(preview)+"updated bucket notifications for %s %s: %s => %s", bucket, name, existingEvents, events)
			}
		}
	}
	//
	buckets, err := S3Client().ListBucketsWithContext(ctx, &s3.ListBucketsInput{})
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	for _, bucket := range buckets.Buckets {
		out, err := S3Client().GetBucketNotificationConfigurationWithContext(ctx, &s3.GetBucketNotificationConfigurationRequest{
			Bucket: bucket.Name,
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		var confs []*s3.LambdaFunctionConfiguration
		for _, conf := range out.LambdaFunctionConfigurations {
			if *conf.LambdaFunctionArn != arnLambda || Contains(triggers, *bucket.Name) {
				confs = append(confs, conf)
			} else {
				Logger.Println(PreviewString(preview)+"remove bucket notification:", name, *bucket.Name)
			}
		}
		if len(confs) != len(out.LambdaFunctionConfigurations) && !preview {
			out.LambdaFunctionConfigurations = confs
			_, err := S3Client().PutBucketNotificationConfigurationWithContext(ctx, &s3.PutBucketNotificationConfigurationInput{
				Bucket:                    bucket.Name,
				NotificationConfiguration: out,
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
	}
	return nil
}

func lambdaAddPermission(ctx context.Context, sid, name, callerPrincipal, callerArn string) error {
	_, err := LambdaClient().AddPermissionWithContext(ctx, &lambda.AddPermissionInput{
		FunctionName: aws.String(name),
		StatementId:  aws.String(sid),
		Action:       aws.String("lambda:InvokeFunction"),
		Principal:    aws.String(callerPrincipal),
		SourceArn:    aws.String(callerArn),
	})
	return err
}

func lambdaEnsurePermission(ctx context.Context, name, callerPrincipal, callerArn string, preview bool) error {
	sid := strings.ReplaceAll(callerPrincipal, ".", "-") + "__" + Last(strings.Split(callerArn, ":"))
	sid = strings.ReplaceAll(sid, "*", "ALL")
	sid = strings.ReplaceAll(sid, "-", "_")
	sid = strings.ReplaceAll(sid, "/", "__")
	var expectedErr error
	var policyString string
	err := Retry(ctx, func() error {
		out, err := LambdaClient().GetPolicyWithContext(ctx, &lambda.GetPolicyInput{
			FunctionName: aws.String(name),
		})
		if err != nil {
			aerr, ok := err.(awserr.Error)
			if ok && aerr.Code() == lambda.ErrCodeResourceNotFoundException {
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
		return err
	}
	if expectedErr != nil {
		if !preview {
			err := lambdaAddPermission(ctx, sid, name, callerPrincipal, callerArn)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"created lambda permission:", name, callerPrincipal, callerArn)
		return nil
	}
	needsUpdate := true
	if policyString != "" {
		policy := IamPolicyDocument{}
		err := json.Unmarshal([]byte(policyString), &policy)
		if err != nil {
			Logger.Println("error:", err)
			return err
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
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"updated lambda permission:", name, callerPrincipal, callerArn)
		return nil
	}
	return nil
}

func LambdaApiUri(ctx context.Context, lambdaName string) (string, error) {
	account, err := StsAccount(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	uri := fmt.Sprintf(
		"arn:aws:apigateway:%s:lambda:path/%s/functions/arn:aws:lambda:%s:%s:function:%s/invocations",
		Region(),
		LambdaClient().APIVersion,
		Region(),
		account,
		lambdaName,
	)
	return uri, nil
}

func LambdaApiUriToLambdaName(apiUri string) string {
	// "arn:aws:apigateway:%s:lambda:path/%s/functions/arn:aws:lambda:%s:%s:function:%s/invocations",
	name := Last(strings.Split(apiUri, ":"))
	name = strings.Split(name, "/")[0]
	return name
}

func LambdaArnToLambdaName(arn string) string {
	// "arn:aws:lambda:%s:%s:function:%s"
	name := Last(strings.Split(arn, ":"))
	return name
}

func LambdaEnsureTriggerApi(ctx context.Context, name string, meta *LambdaMetadata, preview bool) error {
	hasApi := false
	for _, trigger := range meta.Trigger {
		parts := strings.Split(trigger, " ")
		kind := parts[0]
		if kind == lambdaTriggerApi {
			hasApi = true
			restApi, err := Api(ctx, name)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			if restApi == nil {
				if !preview {
					out, err := ApiClient().CreateRestApiWithContext(ctx, &apigateway.CreateRestApiInput{
						Name:             aws.String(name),
						BinaryMediaTypes: apiBinaryMediaTypes,
						EndpointConfiguration: &apigateway.EndpointConfiguration{
							Types: apiEndpointConfigurationTypes,
						},
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
					restApi = out
				}
				Logger.Println(PreviewString(preview)+"created rest api:", name)
			} else {
				if !reflect.DeepEqual(restApi.BinaryMediaTypes, apiBinaryMediaTypes) {
					err := fmt.Errorf("api binary media types misconfigured for %s %s: %v != %v", name, *restApi.Id, StringSlice(restApi.BinaryMediaTypes), StringSlice(apiBinaryMediaTypes))
					Logger.Println("error:", err)
					return err
				}
				if !reflect.DeepEqual(restApi.EndpointConfiguration.Types, apiEndpointConfigurationTypes) {
					err := fmt.Errorf("api endpoint configuration types misconfigured for %s %s: %v != %v", name, *restApi.Id, StringSlice(restApi.EndpointConfiguration.Types), StringSlice(apiEndpointConfigurationTypes))
					Logger.Println("error:", err)
					return err
				}
			}
			if restApi != nil {
				parentID, err := ApiResourceID(ctx, *restApi.Id, "/")
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				if parentID == "" {
					err := fmt.Errorf("api resource id not found for: %s %s /", name, *restApi.Id)
					Logger.Println("error:", err)
					return err
				}
				resourceID, err := ApiResourceID(ctx, *restApi.Id, apiPath)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				if resourceID == "" {
					if !preview {
						out, err := ApiClient().CreateResourceWithContext(ctx, &apigateway.CreateResourceInput{
							RestApiId: aws.String(*restApi.Id),
							ParentId:  aws.String(parentID),
							PathPart:  aws.String(apiPathPart),
						})
						if err != nil {
							Logger.Println("error:", err)
							return err
						}
						resourceID = *out.Id
					}
					Logger.Println(PreviewString(preview)+"created api resource:", *restApi.Id, parentID)
				}
				uri, err := LambdaApiUri(ctx, name)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				for _, id := range []string{parentID, resourceID} {
					outMethod, err := ApiClient().GetMethodWithContext(ctx, &apigateway.GetMethodInput{
						RestApiId:  aws.String(*restApi.Id),
						ResourceId: aws.String(id),
						HttpMethod: aws.String(apiHttpMethod),
					})
					if err != nil {
						aerr, ok := err.(awserr.Error)
						if !ok || aerr.Code() != apigateway.ErrCodeNotFoundException {
							return err
						}
						if !preview {
							_, err := ApiClient().PutMethodWithContext(ctx, &apigateway.PutMethodInput{
								RestApiId:         aws.String(*restApi.Id),
								ResourceId:        aws.String(id),
								HttpMethod:        aws.String(apiHttpMethod),
								AuthorizationType: aws.String(apiAuthType),
							})
							if err != nil {
								Logger.Println("error:", err)
								return err
							}
						}
						Logger.Println(PreviewString(preview)+"created api method:", name, *restApi.Id, id)
					} else {
						if *outMethod.AuthorizationType != apiAuthType {
							err := fmt.Errorf("api method auth type misconfigured for %s %s: %s != %s", name, *restApi, *outMethod.AuthorizationType, apiAuthType)
							Logger.Println("error:", err)
							return err
						}
					}
					outIntegration, err := ApiClient().GetIntegrationWithContext(ctx, &apigateway.GetIntegrationInput{
						RestApiId:  aws.String(*restApi.Id),
						ResourceId: aws.String(id),
						HttpMethod: aws.String(apiHttpMethod),
					})
					if err != nil {
						aerr, ok := err.(awserr.Error)
						if !ok || aerr.Code() != apigateway.ErrCodeNotFoundException {
							return err
						}
						if !preview {
							_, err = ApiClient().PutIntegrationWithContext(ctx, &apigateway.PutIntegrationInput{
								RestApiId:             aws.String(*restApi.Id),
								ResourceId:            aws.String(id),
								HttpMethod:            aws.String(apiHttpMethod),
								Type:                  aws.String(apiType),
								IntegrationHttpMethod: aws.String(apiIntegrationHttpMethod),
								Uri:                   aws.String(uri),
							})
							if err != nil {
								Logger.Println("error:", err)
								return err
							}
						}
						Logger.Println(PreviewString(preview)+"created api integration:", name, *restApi.Id, id)
					} else {
						if *outIntegration.Type != apiType {
							err := fmt.Errorf("api method type misconfigured for %s %s: %s != %s", name, *restApi, *outIntegration.Type, apiType)
							Logger.Println("error:", err)
							return err
						}
						if *outIntegration.Uri != uri {
							err := fmt.Errorf("api method uri misconfigured for %s %s: %s != %s", name, *restApi, *outIntegration.Uri, uri)
							Logger.Println("error:", err)
							return err
						}
					}
				}
				deploymentsOut, err := ApiClient().GetDeploymentsWithContext(ctx, &apigateway.GetDeploymentsInput{
					RestApiId: restApi.Id,
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				if len(deploymentsOut.Items) != 0 && len(deploymentsOut.Items) != 1 {
					err := fmt.Errorf("impossible result for get deployments: %s %d", *restApi.Id, len(deploymentsOut.Items))
					Logger.Println("error:", err)
					return err
				}
				if len(deploymentsOut.Items) != 1 {
					if !preview {
						_, err = ApiClient().CreateDeploymentWithContext(ctx, &apigateway.CreateDeploymentInput{
							RestApiId: restApi.Id,
							StageName: aws.String(apiStageName),
						})
						if err != nil {
							Logger.Println("error:", err)
							return err
						}
					}
					Logger.Println(PreviewString(preview)+"created deployment:", *restApi.Id, apiStageName)
				}
				account, err := StsAccount(ctx)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				arn := fmt.Sprintf("arn:aws:execute-api:%s:%s:%s/*/*/*", Region(), account, *restApi.Id)
				err = lambdaEnsurePermission(ctx, name, "apigateway.amazonaws.com", arn, preview)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			break
		}
	}
	//
	if !hasApi {
		apis, err := apiList(ctx)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}

		for _, api := range apis {
			if *api.Name == name {
				if !preview {
					_, err := ApiClient().DeleteRestApiWithContext(ctx, &apigateway.DeleteRestApiInput{
						RestApiId: api.Id,
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
				}
				Logger.Println(PreviewString(preview)+"deleted api trigger for:", name)
				break
			}
		}
	}
	return nil
}

func lambdaScheduleName(name, schedule string) string {
	return name + "_" + strings.ReplaceAll(base64.StdEncoding.EncodeToString([]byte(schedule)), "=", "")
}

func LambdaEnsureTriggerCloudwatch(ctx context.Context, name, arnLambda string, meta *LambdaMetadata, preview bool) error {
	var triggers []string
	for _, trigger := range meta.Trigger {
		parts := strings.SplitN(trigger, " ", 2)
		kind := parts[0]
		if kind == lambdaTriggerCloudwatch {
			schedule := parts[1]
			triggers = append(triggers, schedule)
		}
	}
	if len(triggers) > 0 {
		for _, schedule := range triggers {
			var scheduleArn string
			scheduleName := lambdaScheduleName(name, schedule)
			out, err := EventsClient().DescribeRuleWithContext(ctx, &cloudwatchevents.DescribeRuleInput{
				Name: aws.String(scheduleName),
			})
			if err != nil {
				aerr, ok := err.(awserr.Error)
				if !ok || aerr.Code() != cloudwatchevents.ErrCodeResourceNotFoundException {
					return err
				}
				if !preview {
					out, err := EventsClient().PutRuleWithContext(ctx, &cloudwatchevents.PutRuleInput{
						Name:               aws.String(scheduleName),
						ScheduleExpression: aws.String(schedule),
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
					scheduleArn = *out.RuleArn
				}
				Logger.Println(PreviewString(preview)+"created cloudwatch rule:", scheduleName, schedule)
			} else {
				if *out.ScheduleExpression != schedule {
					err := fmt.Errorf("cloudwatch rule misconfigured: %s %s != %s", scheduleName, schedule, *out.ScheduleExpression)
					Logger.Println("error:", err)
					return err
				}
				scheduleArn = *out.Arn
			}
			err = lambdaEnsurePermission(ctx, name, "events.amazonaws.com", scheduleArn, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			var targets []*cloudwatchevents.Target
			err = Retry(ctx, func() error {
				var err error
				targets, err = EventsListRuleTargets(ctx, scheduleName)
				return err
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			switch len(targets) {
			case 0:
				if !preview {
					_, err := EventsClient().PutTargetsWithContext(ctx, &cloudwatchevents.PutTargetsInput{
						Rule: aws.String(scheduleName),
						Targets: []*cloudwatchevents.Target{{
							Id:  aws.String("1"),
							Arn: aws.String(arnLambda),
						}},
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
				}
				Logger.Println(PreviewString(preview)+"created cloudwatch rule target:", scheduleName, arnLambda)
			case 1:
				if *targets[0].Arn != arnLambda {
					err := fmt.Errorf("cloudwatch rule is misconfigured with unknown target: %s %s", arnLambda, *targets[0].Arn)
					Logger.Println("error:", err)
					return err
				}
			default:
				var targetArns []string
				for _, target := range targets {
					targetArns = append(targetArns, *target.Arn)
				}
				err := fmt.Errorf("cloudwatch rule is misconfigured with unknown targets: %s %v", arnLambda, targetArns)
				Logger.Println("error:", err)
				return err
			}
		}
	}
	//
	rules, err := EventsListRules(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
outer:
	for _, rule := range rules {
		targets, err := EventsListRuleTargets(ctx, *rule.Name)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		for _, target := range targets {
			if *target.Arn == arnLambda && !Contains(triggers, *rule.ScheduleExpression) {
				if !preview {
					_, err := EventsClient().DeleteRuleWithContext(ctx, &cloudwatchevents.DeleteRuleInput{
						Name: rule.Name,
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
				}
				Logger.Println(PreviewString(preview) + "removed cloudwatch rule")
				continue outer
			}
		}
	}
	return nil
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

func LambdaEnsureTriggerDynamoDB(ctx context.Context, name, arnLambda string, meta *LambdaMetadata, preview bool) error {
	var triggers [][]string
	var triggerTables []string
	for _, trigger := range meta.Trigger {
		parts := strings.Split(trigger, " ")
		kind := parts[0]
		if kind == lambdaTriggerDynamoDB {
			triggerAttrs := parts[1:]
			triggers = append(triggers, triggerAttrs)
			triggerTables = append(triggerTables, triggerAttrs[0])
		}
	}
	if len(triggers) > 0 {
		for _, triggerAttrs := range triggers {
			tableName := triggerAttrs[0]
			triggerAttrs := triggerAttrs[1:]
			streamArn, err := DynamoDBStreamArn(ctx, tableName)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			input := &lambda.CreateEventSourceMappingInput{
				FunctionName:   aws.String(name),
				EventSourceArn: aws.String(streamArn),
				Enabled:        aws.Bool(true),
			}
			for _, line := range triggerAttrs {
				attr, value, err := splitOnce(line, "=")
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
					input.BatchSize = aws.Int64(int64(size))
				case "MaximumBatchingWindowInSeconds":
					size, err := strconv.Atoi(value)
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
					input.MaximumBatchingWindowInSeconds = aws.Int64(int64(size))
				case "MaximumRetryAttempts":
					attempts, err := strconv.Atoi(value)
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
					input.MaximumRetryAttempts = aws.Int64(int64(attempts))
				case "ParallelizationFactor":
					factor, err := strconv.Atoi(value)
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
					input.ParallelizationFactor = aws.Int64(int64(factor))
				case "StartingPosition":
					input.StartingPosition = aws.String(value)
				default:
					err := fmt.Errorf("unknown lambda dynamodb trigger attribute: %s", line)
					Logger.Println("error:", err)
					return err
				}
			}
			eventSourceMappings, err := lambdaListEventSourceMappings(ctx, name, streamArn)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			count := 0
			var found *lambda.EventSourceMappingConfiguration
			for _, mapping := range eventSourceMappings {
				if ArnToInfraName(*mapping.EventSourceArn) == lambdaTriggerDynamoDB {
					if DynamoDBStreamArnToTableName(*mapping.EventSourceArn) == tableName && *mapping.FunctionArn == arnLambda {
						found = mapping
						count++
					}
				}
			}
			switch count {
			case 0:
				if !preview {
					err := Retry(ctx, func() error {
						_, err := LambdaClient().CreateEventSourceMappingWithContext(ctx, input)
						return err
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
				}
				Logger.Println(PreviewString(preview)+"created event source mapping:", name, arnLambda, streamArn, triggerAttrs)
			case 1:
				needsUpdate := false
				update := &lambda.UpdateEventSourceMappingInput{UUID: found.UUID}
				update.FunctionName = input.FunctionName
				if *found.BatchSize != *input.BatchSize {
					Logger.Printf(PreviewString(preview)+"will update lambda event source mapping BatchSize for %s %s: %d => %d", name, tableName, *found.BatchSize, *input.BatchSize)
					update.BatchSize = input.BatchSize
					needsUpdate = true
				}
				if *found.MaximumRetryAttempts != *input.MaximumRetryAttempts {
					Logger.Printf(PreviewString(preview)+"will update lambda event source mapping MaximumRetryAttempts for %s %s: %d => %d", name, tableName, *found.MaximumRetryAttempts, *input.MaximumRetryAttempts)
					update.MaximumRetryAttempts = input.MaximumRetryAttempts
					needsUpdate = true
				}
				if *found.ParallelizationFactor != *input.ParallelizationFactor {
					Logger.Printf(PreviewString(preview)+"will update lambda event source mapping ParallelizationFactor for %s %s: %d => %d", name, tableName, *found.ParallelizationFactor, *input.ParallelizationFactor)
					update.ParallelizationFactor = input.ParallelizationFactor
					needsUpdate = true
				}
				if *found.MaximumBatchingWindowInSeconds != *input.MaximumBatchingWindowInSeconds {
					Logger.Printf(PreviewString(preview)+"will update lambda event source mapping MaximumBatchingWindowInSeconds for %s %s: %d => %d", name, tableName, *found.MaximumBatchingWindowInSeconds, *input.MaximumBatchingWindowInSeconds)
					update.MaximumBatchingWindowInSeconds = input.MaximumBatchingWindowInSeconds
					needsUpdate = true
				}
				if needsUpdate {
					if !preview {
						_, err := LambdaClient().UpdateEventSourceMappingWithContext(ctx, update)
						if err != nil {
							Logger.Println("error:", err)
							return err
						}
					}
					Logger.Println(PreviewString(preview)+"updated event source mapping for %s %s", name, tableName)
				}
			default:
				err := fmt.Errorf("found more than 1 event source mapping for %s %s", name, tableName)
				Logger.Println("error:", err)
				return err
			}
		}
	}
	//
	var marker *string
	for {
		out, err := LambdaClient().ListEventSourceMappingsWithContext(ctx, &lambda.ListEventSourceMappingsInput{
			FunctionName: aws.String(arnLambda),
			Marker:       marker,
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		for _, mapping := range out.EventSourceMappings {
			infra := ArnToInfraName(*mapping.EventSourceArn)
			if infra != lambdaTriggerDynamoDB {
				continue
			}
			tableName := DynamoDBStreamArnToTableName(*mapping.EventSourceArn)
			if !Contains(triggerTables, tableName) {
				if !preview {
					_, err := LambdaClient().DeleteEventSourceMappingWithContext(ctx, &lambda.DeleteEventSourceMappingInput{
						UUID: mapping.UUID,
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
				}
				Logger.Println(PreviewString(preview)+"deleted trigger:", name, tableName)
			}
		}
		if out.NextMarker == nil {
			break
		}
		marker = out.NextMarker
	}
	return nil
}

func lambdaListEventSourceMappings(ctx context.Context, name, eventSourceArn string) ([]*lambda.EventSourceMappingConfiguration, error) {
	var marker *string
	var eventSourceMappings []*lambda.EventSourceMappingConfiguration
	for {
		out, err := LambdaClient().ListEventSourceMappingsWithContext(ctx, &lambda.ListEventSourceMappingsInput{
			FunctionName:   aws.String(name),
			EventSourceArn: aws.String(eventSourceArn),
			Marker:         marker,
		})
		if err != nil {
			return nil, err
		}
		if out.NextMarker == nil {
			break
		}
		marker = out.NextMarker
		eventSourceMappings = append(eventSourceMappings, out.EventSourceMappings...)
	}
	return eventSourceMappings, nil
}

func lambdaSQSTriggerAttrShortcut(s string) string {
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

func LambdaEnsureTriggerSQS(ctx context.Context, name, arnLambda string, meta *LambdaMetadata, preview bool) error {
	var triggers [][]string
	var queueNames []string
	for _, trigger := range meta.Trigger {
		parts := strings.Split(trigger, " ")
		kind := parts[0]
		if kind == lambdaTriggerSQS {
			triggerAttrs := parts[1:]
			triggers = append(triggers, triggerAttrs)
			queueNames = append(queueNames, triggerAttrs[0])
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
				FunctionName:   aws.String(name),
				EventSourceArn: aws.String(sqsArn),
				Enabled:        aws.Bool(true),
			}
			for _, line := range triggerAttrs {
				attr, value, err := splitOnce(line, "=")
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
					input.BatchSize = aws.Int64(int64(size))
				case "MaximumBatchingWindowInSeconds":
					size, err := strconv.Atoi(value)
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
					input.MaximumBatchingWindowInSeconds = aws.Int64(int64(size))
				case "MaximumRetryAttempts":
					attempts, err := strconv.Atoi(value)
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
					input.MaximumRetryAttempts = aws.Int64(int64(attempts))
				case "ParallelizationFactor":
					factor, err := strconv.Atoi(value)
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
					input.ParallelizationFactor = aws.Int64(int64(factor))
				case "StartingPosition":
					input.StartingPosition = aws.String(value)
				default:
					err := fmt.Errorf("unknown lambda dynamodb trigger attribute: %s", line)
					Logger.Println("error:", err)
					return err
				}
			}
			eventSourceMappings, err := lambdaListEventSourceMappings(ctx, name, sqsArn)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			count := 0
			var found *lambda.EventSourceMappingConfiguration
			for _, mapping := range eventSourceMappings {
				if *mapping.EventSourceArn == sqsArn && *mapping.FunctionArn == arnLambda {
					found = mapping
					count++
				}
			}
			switch count {
			case 0:
				if !preview {
					err := Retry(ctx, func() error {
						_, err := LambdaClient().CreateEventSourceMappingWithContext(ctx, input)
						return err
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
				}
				Logger.Println(PreviewString(preview)+"created event source mapping:", name, arnLambda, sqsArn, triggerAttrs)
			case 1:
				needsUpdate := false
				update := &lambda.UpdateEventSourceMappingInput{UUID: found.UUID}
				update.FunctionName = input.FunctionName
				if *found.BatchSize != *input.BatchSize {
					Logger.Printf(PreviewString(preview)+"will update lambda event source mapping BatchSize for %s %s: %d => %d", name, queueName, *found.BatchSize, *input.BatchSize)
					update.BatchSize = input.BatchSize
					needsUpdate = true
				}
				if *found.MaximumRetryAttempts != *input.MaximumRetryAttempts {
					Logger.Printf(PreviewString(preview)+"will update lambda event source mapping MaximumRetryAttempts for %s %s: %d => %d", name, queueName, *found.MaximumRetryAttempts, *input.MaximumRetryAttempts)
					update.MaximumRetryAttempts = input.MaximumRetryAttempts
					needsUpdate = true
				}
				if *found.ParallelizationFactor != *input.ParallelizationFactor {
					Logger.Printf(PreviewString(preview)+"will update lambda event source mapping ParallelizationFactor for %s %s: %d => %d", name, queueName, *found.ParallelizationFactor, *input.ParallelizationFactor)
					update.ParallelizationFactor = input.ParallelizationFactor
					needsUpdate = true
				}
				if *found.MaximumBatchingWindowInSeconds != *input.MaximumBatchingWindowInSeconds {
					Logger.Printf(PreviewString(preview)+"will update lambda event source mapping MaximumBatchingWindowInSeconds for %s %s: %d => %d", name, queueName, *found.MaximumBatchingWindowInSeconds, *input.MaximumBatchingWindowInSeconds)
					update.MaximumBatchingWindowInSeconds = input.MaximumBatchingWindowInSeconds
					needsUpdate = true
				}
				if needsUpdate {
					if !preview {
						_, err := LambdaClient().UpdateEventSourceMappingWithContext(ctx, update)
						if err != nil {
							Logger.Println("error:", err)
							return err
						}
					}
					Logger.Println(PreviewString(preview)+"updated event source mapping for %s %s", name, queueName)
				}
			default:
				err := fmt.Errorf("found more than 1 event source mapping for %s %s", name, queueName)
				Logger.Println("error:", err)
				return err
			}
		}
	}
	//
	var marker *string
	for {
		out, err := LambdaClient().ListEventSourceMappingsWithContext(ctx, &lambda.ListEventSourceMappingsInput{
			FunctionName: aws.String(arnLambda),
			Marker:       marker,
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		for _, mapping := range out.EventSourceMappings {
			infra := ArnToInfraName(*mapping.EventSourceArn)
			if infra != lambdaTriggerSQS {
				continue
			}
			queueName := SQSArnToName(*mapping.EventSourceArn)
			if !Contains(queueNames, queueName) {
				if !preview {
					_, err := LambdaClient().DeleteEventSourceMappingWithContext(ctx, &lambda.DeleteEventSourceMappingInput{
						UUID: mapping.UUID,
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
				}
				Logger.Println(PreviewString(preview)+"deleted trigger:", name, queueName)
			}
		}
		if out.NextMarker == nil {
			break
		}
		marker = out.NextMarker
	}
	return nil
}

func LambdaParseFile(path string, silent bool) (*LambdaMetadata, error) {
	if !Exists(path) {
		err := fmt.Errorf("no such file: %s", path)
		Logger.Println("error:", err)
		return nil, err
	}
	if !strings.HasSuffix(path, ".py") && !strings.HasSuffix(path, ".go") {
		err := fmt.Errorf("only .py or .go files supported: %s", path)
		Logger.Println("error:", err)
		return nil, err
	}
	data, err := ioutil.ReadFile(path)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta, err := LambdaGetMetadata(strings.Split(string(data), "\n"), silent)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	return meta, nil
}

func LambdaZipFile(path string) (string, error) {
	name, err := LambdaName(path)
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	return fmt.Sprintf("/tmp/%s/lambda.zip", name), nil
}

func LambdaCreateZipPy(pth string, requires []string, preview bool) error {
	zipFile, err := LambdaZipFile(pth)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	dir := path.Dir(zipFile)
	if !preview {
		err := shell("rm -rf %s", dir)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = shell("mkdir -p %s", dir)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = shell("virtualenv --python python3 %s/env", dir)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	Logger.Println(PreviewString(preview)+"created virtualenv:", dir)
	if len(requires) > 0 {
		var args []string
		for _, require := range requires {
			args = append(args, fmt.Sprintf(`"%s"`, require))
			Logger.Println(PreviewString(preview)+"require:", require)
		}
		if !preview {
			arg := strings.Join(args, " ")
			err := shell("%s/env/bin/pip install %s", dir, arg)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"pip installed requires with: %s/env/bin/pip", dir)
	}
	if !preview {
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
		err = shellAt(site_package, "cp %s .", pth)
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
		err = shellAt(site_package, "zip -r %s .", zipFile)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	Logger.Println(PreviewString(preview)+"zipped virtualenv:", zipFile)
	return nil
}

func LambdaZipBytes(path string) ([]byte, error) {
	zipFile, err := LambdaZipFile(path)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	data, err := ioutil.ReadFile(zipFile)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	return data, nil
}

func LambdaIncludeInZip(pth string, includes []string, preview bool) error {
	name, err := LambdaName(pth)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	zipFile, err := LambdaZipFile(pth)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	dir := path.Dir(pth)
	for _, include := range includes {
		if strings.Contains(include, "*") {
			paths, err := filepath.Glob(path.Join(dir, include))
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			for _, pth := range paths {
				if !preview {
					err := shellAt(dir, "zip %s %s", zipFile, pth)
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
				}
				Logger.Println(PreviewString(preview)+"include in zip for %s: %s", name, pth)
			}
		} else {
			pth = path.Join(dir, pth)
			if !preview {
				err := shellAt(dir, "zip %s %s", zipFile, pth)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Println(PreviewString(preview)+"include in zip for %s: %s", name, pth)
		}
	}
	return nil
}

func LambdaUpdateZip(pth string, preview bool) error {
	zipFile, err := LambdaZipFile(pth)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if !preview {
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
		err = shellAt(site_package, "cp %s .", pth)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = shellAt(site_package, "zip %s %s", zipFile, path.Base(pth))
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	Logger.Println(PreviewString(preview)+"updated zip:", zipFile, pth)
	return nil
}

func LambdaListFunctions(ctx context.Context) ([]*lambda.FunctionConfiguration, error) {
	Logger.Println("list lambdas")
	var marker *string
	var functions []*lambda.FunctionConfiguration
	for {
		out, err := LambdaClient().ListFunctionsWithContext(ctx, &lambda.ListFunctionsInput{
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

func LambdaEnsure(ctx context.Context, pth string, quick, preview bool) error {
	if strings.HasSuffix(pth, ".py") {
		return lambdaEnsurePy(ctx, pth, quick, preview)
	} else if strings.HasSuffix(pth, ".go") {
		return lambdaEnsureGo(ctx, pth, quick, preview)
	} else if strings.Contains(strings.ToLower(path.Base(pth)), "dockerfile") {
		return lambdaEnsureDockerfile(ctx, pth, quick, preview)
	} else {
		return fmt.Errorf("lambda unknown file type: %s", pth)
	}
}

func lambdaEnsurePy(ctx context.Context, pth string, quick, preview bool) error {
	var err error
	pth, err = filepath.Abs(pth)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	name, err := LambdaName(pth)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	metadata, err := LambdaParseFile(pth, quick)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	zipFile, err := LambdaZipFile(pth)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if quick && Exists(zipFile) {
		err := LambdaUpdateZip(pth, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = LambdaIncludeInZip(pth, metadata.Include, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = LambdaUpdateFunctionZip(ctx, name, pth, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		return nil
	}
	err = LogsEnsureGroup(ctx, name, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = InfraEnsureS3(ctx, metadata.S3, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = InfraEnsureDynamoDB(ctx, metadata.DynamoDB, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = InfraEnsureSqs(ctx, metadata.Sqs, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = IamEnsureRole(ctx, name, "lambda", preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = IamEnsureRolePolicies(ctx, name, metadata.Policy, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = IamEnsureRoleAllows(ctx, name, metadata.Allow, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = LambdaCreateZipPy(pth, metadata.Require, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = LambdaIncludeInZip(pth, metadata.Include, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	//
	concurrency := lambdaAttrConcurrencyDefault
	memory := lambdaAttrMemoryDefault
	timeout := lambdaAttrTimeoutDefault
	//
	for _, attr := range metadata.Attr {
		parts := strings.SplitN(attr, " ", 2)
		k := parts[0]
		v := parts[1]
		switch k {
		case lambdaAttrConcurrency:
			concurrency = atoi(v)
		case lambdaAttrMemory:
			memory = atoi(v)
		case lambdaAttrTimeout:
			timeout = atoi(v)
		default:
			err := fmt.Errorf("unknown attr: %s", k)
			Logger.Println("error:", err)
			return err
		}
	}
	arnRole, err := IamRoleArn(ctx, "lambda", name)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	var zipBytes []byte
	if !preview {
		var err error
		zipBytes, err = LambdaZipBytes(pth)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	input := &lambdaGetOrCreateFunctionInput{
		handler:  strings.TrimSuffix(path.Base(pth), ".py") + ".main",
		name:     name,
		runtime:  "python3.9",
		arnRole:  arnRole,
		timeout:  timeout,
		memory:   memory,
		envVars:  []string{},
		zipBytes: zipBytes,
	}
	arnLambda, err := lambdaGetOrCreateFunction(ctx, input, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	//
	err = LambdaEnsureTriggerDynamoDB(ctx, name, arnLambda, metadata, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = LambdaEnsureTriggerCloudwatch(ctx, name, arnLambda, metadata, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = LambdaEnsureTriggerApi(ctx, name, metadata, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = LambdaEnsureTriggerS3(ctx, name, arnLambda, metadata, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = LambdaEnsureTriggerSQS(ctx, name, arnLambda, metadata, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	//
	err = LambdaSetConcurrency(ctx, name, concurrency, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if !preview {
		err = Retry(ctx, func() error {
			_, err := LambdaClient().UpdateFunctionCodeWithContext(ctx, &lambda.UpdateFunctionCodeInput{
				FunctionName: aws.String(input.name),
				ZipFile:      input.zipBytes,
			})
			return err
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	Logger.Println(PreviewString(preview)+"updated function code:", name)
	//
	env := &lambda.Environment{Variables: make(map[string]*string)}
	if !preview {
		err = Retry(ctx, func() error {
			_, err = LambdaClient().UpdateFunctionConfigurationWithContext(ctx, &lambda.UpdateFunctionConfigurationInput{
				FunctionName: aws.String(input.name),
				Runtime:      aws.String(input.runtime),
				Timeout:      aws.Int64(int64(input.timeout)),
				MemorySize:   aws.Int64(int64(input.memory)),
				Environment:  env,
				Handler:      aws.String(input.handler),
				Role:         aws.String(input.arnRole),
			})
			return err
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	Logger.Println(PreviewString(preview)+"updated function configuration:", name)
	return nil
}

type lambdaGetOrCreateFunctionInput struct {
	handler  string
	name     string
	runtime  string
	arnRole  string
	timeout  int
	memory   int
	envVars  []string
	zipBytes []byte
}

func lambdaGetOrCreateFunction(ctx context.Context, input *lambdaGetOrCreateFunctionInput, preview bool) (string, error) {
	var expectedErr error
	var arn string
	err := Retry(ctx, func() error {
		out, err := LambdaClient().GetFunctionWithContext(ctx, &lambda.GetFunctionInput{
			FunctionName: aws.String(input.name),
		})
		if err != nil {
			aerr, ok := err.(awserr.Error)
			if !ok || aerr.Code() != lambda.ErrCodeResourceNotFoundException {
				return err
			}
			expectedErr = err
			return nil
		}
		arn = *out.Configuration.FunctionArn
		return nil
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	if expectedErr == nil {
		return arn, nil
	}
	if !preview {
		env := &lambda.Environment{Variables: make(map[string]*string)}
		err = Retry(ctx, func() error {
			out, err := LambdaClient().CreateFunctionWithContext(ctx, &lambda.CreateFunctionInput{
				FunctionName: aws.String(input.name),
				Runtime:      aws.String(input.runtime),
				Timeout:      aws.Int64(int64(input.timeout)),
				MemorySize:   aws.Int64(int64(input.memory)),
				Environment:  env,
				Handler:      aws.String(input.handler),
				Role:         aws.String(input.arnRole),
				Code: &lambda.FunctionCode{ZipFile: input.zipBytes},
			})
			if err != nil {
				return err
			}
			arn = *out.FunctionArn
			return nil
		})
		if err != nil {
			Logger.Println("error:", err)
			return "", err
		}
	}
	Logger.Println(PreviewString(preview) + "created function: " + input.name)
	return arn, nil
}

func LambdaUpdateFunctionZip(ctx context.Context, name, pth string, preview bool) error {
	zipBytes, err := LambdaZipBytes(pth)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if !preview {
		var expectedErr error
		err = Retry(ctx, func() error {
			_, err := LambdaClient().UpdateFunctionCodeWithContext(ctx, &lambda.UpdateFunctionCodeInput{
				FunctionName: aws.String(name),
				ZipFile:      zipBytes,
			})
			if err != nil {
				aerr, ok := err.(awserr.Error)
				if !ok || aerr.Code() != lambda.ErrCodeResourceNotFoundException {
					return err
				}
				expectedErr = err
				return nil
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
	Logger.Println(PreviewString(preview) + "lambda updated code zipfile for: " + name)
	return nil
}

func lambdaEnsureGo(ctx context.Context, pth string, quick, preview bool) error {
	_ = ctx
	_ = pth
	_ = quick
	_ = preview
	return nil
}

func lambdaEnsureDockerfile(ctx context.Context, pth string, quick, preview bool) error {
	_ = ctx
	_ = pth
	_ = quick
	_ = preview
	return nil
}
