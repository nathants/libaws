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
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/acm"
	"github.com/aws/aws-sdk-go/service/apigatewayv2"
	"github.com/aws/aws-sdk-go/service/cloudwatchevents"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/aws/aws-sdk-go/service/s3"
)

const (
	lambdaAttrName        = "name"
	lambdaAttrConcurrency = "concurrency"
	lambdaAttrMemory      = "memory"
	lambdaAttrTimeout     = "timeout"
	lambdaAttrLogsTTLDays = "logs-ttl-days"

	lambdaAttrConcurrencyDefault = 0
	lambdaAttrMemoryDefault      = 128
	lambdaAttrTimeoutDefault     = 300
	lambdaAttrLogsTTLDaysDefault = 7

	lambdaTriggerSQS        = "sqs"
	lambdaTrigerS3          = "s3"
	lambdaTriggerDynamoDB   = "dynamodb"
	lambdaTriggerCloudwatch = "cloudwatch"
	lambdaTriggerApi        = "api"
	lambdaTriggerWebsocket  = "websocket"

	lambdaTriggerApiAttrDns    = "dns"
	lambdaTriggerApiAttrDomain = "domain"

	lambdaMetaS3       = "s3"
	lambdaMetaDynamoDB = "dynamodb"
	lambdaMetaSQS      = "sqs"
	lambdaMetaPolicy   = "policy"
	lambdaMetaAllow    = "allow"
	lambdaMetaInclude  = "include"
	lambdaMetaTrigger  = "trigger"
	lambdaMetaRequire  = "require"
	lambdaMetaAttr     = "attr"
	lambdaMetaEnv      = "env"

	lambdaDollarDefault     = "$default"
	lambdaDollarConnect     = "$connect"
	lambdaDollarDisconnect  = "$disconnect"
	lambdaAuthorizationType = "NONE"
	lambdaRouteSelection    = "${request.body.action}"
	lambdaIntegrationMethod = "POST"
	lambdaPayloadVersion    = "1.0"
	lambdaWebsocketSuffix   = "-websocket"
)

var lambdaClient *lambda.Lambda
var lambdaClientLock sync.RWMutex

func LambdaClientExplicit(accessKeyID, accessKeySecret, region string) *lambda.Lambda {
	return lambda.New(SessionExplicit(accessKeyID, accessKeySecret, region))
}

func LambdaClient() *lambda.Lambda {
	lambdaClientLock.Lock()
	defer lambdaClientLock.Unlock()
	if lambdaClient == nil {
		lambdaClient = lambda.New(Session())
	}
	return lambdaClient
}

func LambdaSetConcurrency(ctx context.Context, name string, concurrency int, preview bool) error {
	out, err := LambdaClient().GetFunctionConcurrencyWithContext(ctx, &lambda.GetFunctionConcurrencyInput{
		FunctionName: aws.String(name),
	})
	if err != nil && !preview {
		Logger.Println("error:", err)
		return err
	}
	if out.ReservedConcurrentExecutions == nil {
		out.ReservedConcurrentExecutions = aws.Int64(0)
	}
	if int(*out.ReservedConcurrentExecutions) != concurrency {
		if !preview {
			if concurrency > 0 {
				_, err := LambdaClient().PutFunctionConcurrencyWithContext(ctx, &lambda.PutFunctionConcurrencyInput{
					FunctionName:                 aws.String(name),
					ReservedConcurrentExecutions: aws.Int64(int64(concurrency)),
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			} else {
				_, err := LambdaClient().DeleteFunctionConcurrencyWithContext(ctx, &lambda.DeleteFunctionConcurrencyInput{
					FunctionName: aws.String(name),
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}

			}
		}
		Logger.Printf(PreviewString(preview)+"updated concurrency: %s %d => %d\n", name, *out.ReservedConcurrentExecutions, concurrency)
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
	metadata, err := LambdaParseFile(pth)
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	for _, attr := range metadata.Attr {
		parts := strings.SplitN(attr, " ", 2)
		k := parts[0]
		v := parts[1]
		switch k {
		case lambdaAttrName:
			return v, nil
		}
	}
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
		return "", expectedErr
	}
	return arn, nil
}

func lambdaFilterMetadata(lines []string) []string {
	var res []string
	for _, line := range lines {
		line = strings.Trim(line, "\n")
		if len(line) >= 2 && line[:2] == "#!" {
			continue
		}
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			line = strings.Trim(line, "#/ ")
			line = strings.Split(line, " #")[0]
			line = strings.Split(line, " //")[0]
			line = strings.Trim(line, " ")
			line = regexp.MustCompile(` +`).ReplaceAllString(line, " ")
			res = append(res, line)
		}
		parts := strings.Split(line, " ")
		if len(parts) > 0 && Contains([]string{"import", "def", "func"}, parts[0]) {
			break
		}
	}
	return res
}

func lambdaParseMetadata(token string, lines []string) ([]string, []string, error) {
	token = token + ":"
	var vals [][]string
	previousMatch := false
	for _, line := range lambdaFilterMetadata(lines) {
		if strings.HasPrefix(line, token) {
			previousMatch = true
			part := Last(strings.SplitN(line, token, 2))
			part = strings.Trim(part, " ")
			vals = append(vals, []string{line, part})
		} else if previousMatch && strings.HasPrefix(line, "- ") && len(vals) > 0 {
			last := vals[len(vals)-1]
			last[1] = last[1] + " " + line[2:]
			vals[len(vals)-1] = last
		} else {
			previousMatch = false
		}
	}
	var results []string
	var envVars []string
	for _, val := range vals {
		line := val[0]
		part := val[1]
		for _, variable := range regexp.MustCompile(`(\$\{[^\}]+})`).FindAllString(part, -1) {
			variableName := variable[2 : len(variable)-1]
			variableValue := os.Getenv(variableName)
			if variableValue == "" {
				err := fmt.Errorf("missing environment variable: %s", line)
				Logger.Println("error:", err)
				return nil, nil, err
			}
			envVars = append(envVars, fmt.Sprintf("%s=%s", variableName, variableValue))
			part = strings.Replace(part, variable, variableValue, 1)
		}
		results = append(results, part)
	}
	return results, envVars, nil
}

type LambdaMetadata struct {
	S3       []string `json:"s3,omitempty"`
	DynamoDB []string `json:"dynamodb,omitempty"`
	Sqs      []string `json:"sqs,omitempty"`
	Policy   []string `json:"policy,omitempty"`
	Allow    []string `json:"allow,omitempty"`
	Include  []string `json:"include,omitempty"`
	Trigger  []string `json:"trigger,omitempty"`
	Require  []string `json:"require,omitempty"`
	Attr     []string `json:"attr,omitempty"`
	Env      []string `json:"env,omitempty"`
}

func LambdaGetMetadata(lines []string) (*LambdaMetadata, error) {
	var err error
	meta := &LambdaMetadata{}
	var envVars []string
	meta.S3, envVars, err = lambdaParseMetadata(lambdaMetaS3, lines)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Env = append(meta.Env, envVars...)
	meta.DynamoDB, envVars, err = lambdaParseMetadata(lambdaMetaDynamoDB, lines)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Env = append(meta.Env, envVars...)
	meta.Sqs, envVars, err = lambdaParseMetadata(lambdaMetaSQS, lines)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Env = append(meta.Env, envVars...)
	meta.Policy, envVars, err = lambdaParseMetadata(lambdaMetaPolicy, lines)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Env = append(meta.Env, envVars...)
	meta.Allow, envVars, err = lambdaParseMetadata(lambdaMetaAllow, lines)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Env = append(meta.Env, envVars...)
	meta.Include, envVars, err = lambdaParseMetadata(lambdaMetaInclude, lines)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Env = append(meta.Env, envVars...)
	meta.Trigger, envVars, err = lambdaParseMetadata(lambdaMetaTrigger, lines)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Env = append(meta.Env, envVars...)
	meta.Require, envVars, err = lambdaParseMetadata(lambdaMetaRequire, lines)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Env = append(meta.Env, envVars...)
	meta.Attr, envVars, err = lambdaParseMetadata(lambdaMetaAttr, lines)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Env = append(meta.Env, envVars...)
	var extraEnv []string
	extraEnv, envVars, err = lambdaParseMetadata(lambdaMetaEnv, lines)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Env = append(meta.Env, envVars...)
	meta.Env = append(meta.Env, extraEnv...)
	for _, conf := range meta.Attr {
		parts := strings.SplitN(conf, " ", 2)
		k := parts[0]
		v := parts[1]
		if !Contains([]string{lambdaAttrName, lambdaAttrConcurrency, lambdaAttrMemory, lambdaAttrTimeout, lambdaAttrLogsTTLDays}, k) {
			err := fmt.Errorf("unknown attr: %s", k)
			Logger.Println("error:", err)
			return nil, err
		}
		if !IsDigit(v) && k != lambdaAttrName {
			err := fmt.Errorf("conf value should be digits: %s %s", k, v)
			Logger.Println("error:", err)
			return nil, err
		}
	}
	for _, trigger := range meta.Trigger {
		if !Contains([]string{lambdaTriggerSQS, lambdaTrigerS3, lambdaTriggerDynamoDB, lambdaTriggerApi, lambdaTriggerCloudwatch, lambdaTriggerWebsocket}, strings.Split(trigger, " ")[0]) {
			err := fmt.Errorf("unknown trigger: %s", trigger)
			Logger.Println("error:", err)
			return nil, err
		}
	}
	for _, line := range lambdaFilterMetadata(lines) {
		if strings.Trim(line, " ") == "" {
			continue
		}
		token := strings.SplitN(line, ":", 2)[0]
		if strings.HasPrefix(line, "- ") || !Contains([]string{lambdaMetaS3, lambdaMetaDynamoDB, lambdaMetaSQS, lambdaMetaPolicy, lambdaMetaAllow, lambdaMetaInclude, lambdaMetaTrigger, lambdaMetaRequire, lambdaMetaAttr, lambdaMetaEnv}, token) {
			err := fmt.Errorf("unknown configuration comment: %s", line)
			Logger.Println("error:", err)
			return nil, err
		}
	}
	return meta, nil
}

func LambdaEnsureTriggerS3(ctx context.Context, name, arnLambda string, meta *LambdaMetadata, preview bool) error {
	events := []*string{
		aws.String("s3:ObjectCreated:*"),
		aws.String("s3:ObjectRemoved:*"),
	}
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
					if *conf.LambdaFunctionArn != arnLambda {
						confs = append(confs, conf)
					}
				}
				confs = append(confs, &s3.LambdaFunctionConfiguration{
					LambdaFunctionArn: aws.String(arnLambda),
					Events:            events,
				})
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
				Logger.Printf(PreviewString(preview)+"updated bucket notifications for %s %s: %s => %s\n", bucket, name, StringSlice(existingEvents), StringSlice(events))
			}
		}
	}
	buckets, err := S3Client().ListBucketsWithContext(ctx, &s3.ListBucketsInput{})
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	for _, bucket := range buckets.Buckets {
		out, err := S3ClientBucketRegionMust(*bucket.Name).GetBucketNotificationConfigurationWithContext(ctx, &s3.GetBucketNotificationConfigurationRequest{
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
			_, err := S3ClientBucketRegionMust(*bucket.Name).PutBucketNotificationConfigurationWithContext(ctx, &s3.PutBucketNotificationConfigurationInput{
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
	region := Region()
	account, err := StsAccount(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	_, err = LambdaClient().AddPermissionWithContext(ctx, &lambda.AddPermissionInput{
		FunctionName: aws.String(fmt.Sprintf("arn:aws:lambda:%s:%s:function:%s", region, account, name)),
		StatementId:  aws.String(sid),
		Action:       aws.String("lambda:InvokeFunction"),
		Principal:    aws.String(callerPrincipal),
		SourceArn:    aws.String(callerArn),
	})
	return err
}

func lambdaEnsurePermission(ctx context.Context, name, callerPrincipal, callerArn string, preview bool) error {
	sid := strings.ReplaceAll(callerPrincipal, ".", "-") + "__" + Last(strings.Split(callerArn, ":"))
	sid = strings.ReplaceAll(sid, "$", "DOLLAR")
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

func lambdaEnsureTriggerApi(ctx context.Context, name, arnLambda string, protocolType string, preview bool) (*apigatewayv2.Api, error) {
	if !Contains([]string{apigatewayv2.ProtocolTypeHttp, apigatewayv2.ProtocolTypeWebsocket}, protocolType) {
		err := fmt.Errorf("invalid protocol type: %s", protocolType)
		Logger.Println("error:", err)
		return nil, err
	}
	api, err := Api(ctx, name)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	if api == nil {
		if !preview {
			input := &apigatewayv2.CreateApiInput{
				Name:         aws.String(name),
				ProtocolType: aws.String(protocolType),
			}
			if protocolType == apigatewayv2.ProtocolTypeWebsocket {
				input.RouteKey = aws.String(lambdaDollarDefault)
				input.Target = aws.String(arnLambda)
				input.RouteSelectionExpression = aws.String(lambdaRouteSelection)
			}
			_, err := ApiClient().CreateApiWithContext(ctx, input)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			httpApi, err := Api(ctx, name)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			return httpApi, nil
		}
		Logger.Println(PreviewString(preview)+"created api:", name)
		return nil, nil
	}
	if *api.ProtocolType != protocolType {
		err := fmt.Errorf("api protocol type misconfigured for %s %s: %v != %v", name, *api.ApiId, *api.ProtocolType, protocolType)
		Logger.Println("error:", err)
		return nil, err
	}
	return api, nil
}

func lambdaEnsureTriggerApiIntegrationStageRoute(ctx context.Context, name, arnLambda, protocolType string, api *apigatewayv2.Api, timeoutMillis int64, preview bool) error {
	if api == nil && preview {
		Logger.Println(PreviewString(preview)+"created api integration:", name)
		Logger.Println(PreviewString(preview)+"created api stage:", name)
		Logger.Println(PreviewString(preview)+"created api route:", name)
		return nil
	}
	account, err := StsAccount(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	var integrationId string
	getIntegrationsOut, err := ApiClient().GetIntegrationsWithContext(ctx, &apigatewayv2.GetIntegrationsInput{
		ApiId:      api.ApiId,
		MaxResults: aws.String(fmt.Sprint(500)),
	})
	if err != nil || len(getIntegrationsOut.Items) == 500 {
		Logger.Println("error:", err)
		return err
	}
	switch len(getIntegrationsOut.Items) {
	case 0:
		if !preview {
			out, err := ApiClient().CreateIntegrationWithContext(ctx, &apigatewayv2.CreateIntegrationInput{
				ApiId:                api.ApiId,
				IntegrationUri:       aws.String(arnLambda),
				ConnectionType:       aws.String(apigatewayv2.ConnectionTypeInternet),
				IntegrationType:      aws.String(apigatewayv2.IntegrationTypeAwsProxy),
				IntegrationMethod:    aws.String(lambdaIntegrationMethod),
				TimeoutInMillis:      aws.Int64(timeoutMillis),
				PayloadFormatVersion: aws.String(lambdaPayloadVersion),
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			integrationId = *out.IntegrationId
		}
		Logger.Println(PreviewString(preview)+"created api integration:", name)
	case 1:
		integration := getIntegrationsOut.Items[0]
		integrationId = *integration.IntegrationId
		if *integration.ConnectionType != apigatewayv2.ConnectionTypeInternet {
			err := fmt.Errorf("api connection type misconfigured for %s %s: %s != %s", name, *api.ApiId, *integration.ConnectionType, apigatewayv2.ConnectionTypeInternet)
			Logger.Println("error:", err)
			return err
		}
		if *integration.IntegrationType != apigatewayv2.IntegrationTypeAwsProxy {
			err := fmt.Errorf("api integration type misconfigured for %s %s: %s != %s", name, *api.ApiId, *integration.IntegrationType, apigatewayv2.IntegrationTypeAwsProxy)
			Logger.Println("error:", err)
			return err
		}
		if *integration.IntegrationMethod != lambdaIntegrationMethod {
			err := fmt.Errorf("api integration method misconfigured for %s %s: %s != %s", name, *api.ApiId, *integration.IntegrationMethod, lambdaIntegrationMethod)
			Logger.Println("error:", err)
			return err
		}
		if *integration.TimeoutInMillis != timeoutMillis {
			err := fmt.Errorf("api timeout misconfigured for %s %s: %d != %d", name, *api.ApiId, *integration.TimeoutInMillis, timeoutMillis)
			Logger.Println("error:", err)
			return err
		}
		if *integration.PayloadFormatVersion != lambdaPayloadVersion {
			err := fmt.Errorf("api payload format version misconfigured for %s %s: %s != %s", name, *api.ApiId, *integration.PayloadFormatVersion, lambdaPayloadVersion)
			Logger.Println("error:", err)
			return err
		}
	default:
		err := fmt.Errorf("api has more than one integration: %s %v", name, Pformat(getIntegrationsOut.Items))
		Logger.Println("error:", err)
		return err
	}
	getStageOut, err := ApiClient().GetStageWithContext(ctx, &apigatewayv2.GetStageInput{
		ApiId:     api.ApiId,
		StageName: aws.String(lambdaDollarDefault),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != apigatewayv2.ErrCodeNotFoundException {
			Logger.Println("error:", err)
			return err
		}
		if !preview {
			_, err := ApiClient().CreateStageWithContext(ctx, &apigatewayv2.CreateStageInput{
				ApiId:      api.ApiId,
				AutoDeploy: aws.Bool(true),
				StageName:  aws.String(lambdaDollarDefault),
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"created api stage:", name)
	} else {
		if *getStageOut.StageName != lambdaDollarDefault {
			err := fmt.Errorf("api stage name misconfigured for %s %s: %s != %s", name, *api.ApiId, *getStageOut.StageName, lambdaDollarDefault)
			Logger.Println("error:", err)
			return err
		}
		if !*getStageOut.AutoDeploy {
			err := fmt.Errorf("api stage auto deploy misconfigured for %s %s, should be enabled", name, *api.ApiId)
			Logger.Println("error:", err)
			return err
		}
	}
	getRoutesOut, err := ApiClient().GetRoutesWithContext(ctx, &apigatewayv2.GetRoutesInput{
		ApiId:      api.ApiId,
		MaxResults: aws.String(fmt.Sprint(500)),
	})
	if err != nil || len(getRoutesOut.Items) == 500 {
		Logger.Println("error:", err)
		return err
	}
	var routeKeys []string
	if protocolType == apigatewayv2.ProtocolTypeHttp {
		routeKeys = []string{lambdaDollarDefault}
	} else if protocolType == apigatewayv2.ProtocolTypeWebsocket {
		routeKeys = []string{lambdaDollarDefault, lambdaDollarConnect, lambdaDollarDisconnect}
	}
	for _, routeKey := range routeKeys {
		var routes []*apigatewayv2.Route
		for _, route := range getRoutesOut.Items {
			if *route.RouteKey == routeKey {
				routes = append(routes, route)
			}
		}
		switch len(routes) {
		case 0:
			if !preview {
				_, err := ApiClient().CreateRouteWithContext(ctx, &apigatewayv2.CreateRouteInput{
					ApiId:             api.ApiId,
					Target:            aws.String(fmt.Sprintf("integrations/%s", integrationId)),
					RouteKey:          aws.String(routeKey),
					AuthorizationType: aws.String(lambdaAuthorizationType),
					ApiKeyRequired:    aws.Bool(false),
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Println(PreviewString(preview)+"created api route:", name, routeKey)
		case 1:
			route := routes[0]
			if *route.Target != fmt.Sprintf("integrations/%s", integrationId) {
				err := fmt.Errorf("api route target misconfigured for %s %s: %s != %s", name, *api.ApiId, *route.Target, fmt.Sprintf("integrations/%s", integrationId))
				Logger.Println("error:", err)
				return err
			}
			if *route.RouteKey != routeKey {
				err := fmt.Errorf("api route key misconfigured for %s %s: %s != %s", name, *api.ApiId, *route.RouteKey, routeKey)
				Logger.Println("error:", err)
				return err
			}
			if *route.AuthorizationType != lambdaAuthorizationType {
				err := fmt.Errorf("api route authorization type misconfigured for %s %s: %s != %s", name, *api.ApiId, *route.AuthorizationType, lambdaAuthorizationType)
				Logger.Println("error:", err)
				return err
			}
			if *route.ApiKeyRequired {
				err := fmt.Errorf("api route apiKeyRequired misconfigured for %s %s, should be disabled", name, *api.ApiId)
				Logger.Println("error:", err)
				return err
			}
		default:
			err := fmt.Errorf("api has more than one route: %s %s %v", name, routeKey, Pformat(routes))
			Logger.Println("error:", err)
			return err
		}
	}
	arn := fmt.Sprintf("arn:aws:execute-api:%s:%s:%s/*/*", Region(), account, *api.ApiId)
	lambdaName := Last(strings.Split(arnLambda, ":"))
	err = lambdaEnsurePermission(ctx, lambdaName, "apigateway.amazonaws.com", arn, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	return nil
}

func lambdaEnsureTriggerApiDomainName(ctx context.Context, name, domain string, preview bool) error {
	out, err := ApiClient().GetDomainNameWithContext(ctx, &apigatewayv2.GetDomainNameInput{
		DomainName: aws.String(domain),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != apigatewayv2.ErrCodeNotFoundException {
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
				out, err := AcmClient().DescribeCertificateWithContext(ctx, &acm.DescribeCertificateInput{
					CertificateArn: cert.CertificateArn,
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				wildcard := fmt.Sprintf("*.%s", parentDomain)
				if Contains(StringSlice(out.Certificate.SubjectAlternativeNames), wildcard) {
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
			_, err = ApiClient().CreateDomainNameWithContext(ctx, &apigatewayv2.CreateDomainNameInput{
				DomainName: aws.String(domain),
				DomainNameConfigurations: []*apigatewayv2.DomainNameConfiguration{{
					ApiGatewayDomainName: aws.String(domain),
					CertificateArn:       aws.String(arnCert),
					EndpointType:         aws.String(apigatewayv2.EndpointTypeRegional),
					SecurityPolicy:       aws.String(apigatewayv2.SecurityPolicyTls12),
				}},
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"created api domain:", name, domain)
	} else {
		if len(out.DomainNameConfigurations) != 1 || *out.DomainNameConfigurations[0].EndpointType != apigatewayv2.EndpointTypeRegional {
			err := fmt.Errorf("api endpoint type misconfigured: %s", Pformat(out.DomainNameConfigurations))
			Logger.Println("error:", err)
			return err
		}
		if out.DomainNameConfigurations[0].SecurityPolicy == nil || *out.DomainNameConfigurations[0].SecurityPolicy != apigatewayv2.SecurityPolicyTls12 {
			err := fmt.Errorf("api security policy misconfigured: %s", Pformat(out.DomainNameConfigurations[0].SecurityPolicy))
			Logger.Println("error:", err)
			return err
		}
	}
	return nil
}

func lambdaEnsureTriggerApiDnsRecords(ctx context.Context, name, subDomain string, zone *route53.HostedZone, preview bool) error {
	out, err := ApiClient().GetDomainNameWithContext(ctx, &apigatewayv2.GetDomainNameInput{
		DomainName: aws.String(subDomain),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != apigatewayv2.ErrCodeNotFoundException {
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
		for _, record := range records {
			if strings.TrimRight(*record.Name, ".") == subDomain && *record.Type == route53.RRTypeA {
				found = true
				if strings.TrimRight(*record.AliasTarget.DNSName, ".") != *out.DomainNameConfigurations[0].ApiGatewayDomainName {
					err := fmt.Errorf("alias target misconfigured: %s != %s", *record.AliasTarget.DNSName, *out.DomainNameConfigurations[0].ApiGatewayDomainName)
					Logger.Println("error:", err)
					return err
				}
			}
		}
		if !found {
			if !preview {
				_, err := Route53Client().ChangeResourceRecordSetsWithContext(ctx, &route53.ChangeResourceRecordSetsInput{
					HostedZoneId: zone.Id,
					ChangeBatch: &route53.ChangeBatch{
						Changes: []*route53.Change{{
							Action: aws.String(route53.ChangeActionUpsert),
							ResourceRecordSet: &route53.ResourceRecordSet{
								Name: aws.String(subDomain),
								Type: aws.String(route53.RRTypeA),
								AliasTarget: &route53.AliasTarget{
									DNSName:              out.DomainNameConfigurations[0].ApiGatewayDomainName,
									HostedZoneId:         out.DomainNameConfigurations[0].HostedZoneId,
									EvaluateTargetHealth: aws.Bool(false),
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
			Logger.Println(PreviewString(preview)+"created api dns:", name, subDomain)
		}
	}
	return nil
}

func lambdaEnsureTriggerApiDns(ctx context.Context, name, domain string, api *apigatewayv2.Api, preview bool) error {
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
		subDomain := domain
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
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
		err = fmt.Errorf("no zone found matching domain or parent domain: %s", domain)
		Logger.Println("error:", err)
		return err
	}
	return nil
}

func lambdaEnsureTriggerApiMapping(ctx context.Context, name, subDomain string, api *apigatewayv2.Api, preview bool) error {
	mappings, err := ApiClient().GetApiMappingsWithContext(ctx, &apigatewayv2.GetApiMappingsInput{
		DomainName: aws.String(subDomain),
		MaxResults: aws.String(fmt.Sprint(500)),
	})
	if err != nil || len(mappings.Items) == 500 {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != apigatewayv2.ErrCodeNotFoundException {
			Logger.Println("error:", err)
			return err
		}
	}
	switch len(mappings.Items) {
	case 0:
		if !preview {
			_, err := ApiClient().CreateApiMappingWithContext(ctx, &apigatewayv2.CreateApiMappingInput{
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
	case 1:
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

func LambdaEnsureTriggerApi(ctx context.Context, name string, meta *LambdaMetadata, preview bool) error {
	hasApi := false
	hasWebsocket := false
	domainApi := ""
	domainWebsocket := ""
	account, err := StsAccount(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	arnLambda := fmt.Sprintf("arn:aws:lambda:%s:%s:function:%s", Region(), account, name)
	count := 0
	for _, trigger := range meta.Trigger {
		var protocolType string
		var domainName string
		var apiName string
		var timeoutMillis int64
		parts := strings.Split(trigger, " ")
		kind := parts[0]
		attrs := parts[1:]
		if kind == lambdaTriggerApi || kind == lambdaTriggerWebsocket {
			time.Sleep(time.Duration(count) * 5 * time.Second) // very low api limits on apigateway create domain, sleep here is we are adding more than 1
			count++
			if kind == lambdaTriggerApi {
				apiName = name
				if hasApi {
					err := fmt.Errorf("cannot have more than one api trigger")
					Logger.Println("error:", err)
					return err
				}
				hasApi = true
				protocolType = apigatewayv2.ProtocolTypeHttp
				timeoutMillis = 30000
			} else if kind == lambdaTriggerWebsocket {
				apiName = name + lambdaWebsocketSuffix
				if hasWebsocket {
					err := fmt.Errorf("cannot have more than one websocket trigger")
					Logger.Println("error:", err)
					return err
				}
				hasWebsocket = true
				protocolType = apigatewayv2.ProtocolTypeWebsocket
				timeoutMillis = 29000
			}
			api, err := lambdaEnsureTriggerApi(ctx, apiName, arnLambda, protocolType, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			err = lambdaEnsureTriggerApiIntegrationStageRoute(ctx, apiName, arnLambda, protocolType, api, timeoutMillis, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			for _, attr := range attrs {
				k, v, err := SplitOnce(attr, "=")
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				switch k {
				case lambdaTriggerApiAttrDns: // apigateway custom domain + route53
					domainName = v
					err := lambdaEnsureTriggerApiDns(ctx, apiName, domainName, api, preview)
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
				case lambdaTriggerApiAttrDomain: // apigateway custom domain
					domainName = v
					err := lambdaEnsureTriggerApiDomainName(ctx, apiName, domainName, preview)
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
					err = lambdaEnsureTriggerApiMapping(ctx, apiName, domainName, api, preview)
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
				default:
					err := fmt.Errorf("unknown attr: %s", attr)
					Logger.Println("error:", err)
					return err
				}
				if kind == lambdaTriggerApi {
					domainApi = domainName
				} else if kind == lambdaTriggerWebsocket {
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
			apiName = name
			apiDomain = domainApi
			apiEnsured = hasApi
		} else if kind == lambdaTriggerWebsocket {
			apiName = name + lambdaWebsocketSuffix
			apiDomain = domainWebsocket
			apiEnsured = hasWebsocket
		}
		api, err := Api(ctx, apiName)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		if api != nil {
			time.Sleep(time.Duration(count) * 5 * time.Second) // very low api limits on apigateway delete domain, sleep here is we are adding more than 1
			count++
			domains, err := ApiListDomains(ctx)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			// delete any unused domains
			for _, domain := range domains {
				if *domain.DomainName == apiDomain {
					continue
				}
				err := lambdaTriggerApiDeleteDns(ctx, apiName, api, domain, preview)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			// if api trigger unused, delete rest api
			if !apiEnsured {
				err = lambdaTriggerApiDeleteApi(ctx, apiName, api, preview)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
		}
	}
	return nil
}

func lambdaTriggerApiDeleteApi(ctx context.Context, name string, api *apigatewayv2.Api, preview bool) error {
	if !preview {
		_, err := ApiClient().DeleteApiWithContext(ctx, &apigatewayv2.DeleteApiInput{
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

func lambdaTriggerApiDeleteDns(ctx context.Context, name string, api *apigatewayv2.Api, domain *apigatewayv2.DomainName, preview bool) error {
	mappings, err := ApiClient().GetApiMappingsWithContext(ctx, &apigatewayv2.GetApiMappingsInput{
		DomainName: domain.DomainName,
		MaxResults: aws.String(fmt.Sprint(500)),
	})
	if err != nil || len(mappings.Items) == 500 {
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
						strings.TrimRight(*record.AliasTarget.DNSName, ".") == *domain.DomainNameConfigurations[0].ApiGatewayDomainName
					nameMatch := record.Name != nil &&
						strings.TrimRight(*record.Name, ".") == *domain.DomainName
					if targetMatch && nameMatch {
						if !preview {
							_, err := Route53Client().ChangeResourceRecordSetsWithContext(ctx, &route53.ChangeResourceRecordSetsInput{
								HostedZoneId: zone.Id,
								ChangeBatch: &route53.ChangeBatch{Changes: []*route53.Change{{
									Action:            aws.String(route53.ChangeActionDelete),
									ResourceRecordSet: record,
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
				_, err := ApiClient().DeleteDomainNameWithContext(ctx, &apigatewayv2.DeleteDomainNameInput{
					DomainName: domain.DomainName,
				})
				if err != nil {
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
				aerr, ok := err.(awserr.Error)
				if ok && aerr.Code() == "ResourceNotFoundException" {
					return nil
				}
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
	rules, err := EventsListRules(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	for _, rule := range rules {
		targets, err := EventsListRuleTargets(ctx, *rule.Name)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		for _, target := range targets {
			if *target.Arn == arnLambda && !Contains(triggers, *rule.ScheduleExpression) {
				if !preview {
					ids := []*string{}
					for _, target := range targets {
						ids = append(ids, target.Id)
					}
					_, err := EventsClient().RemoveTargetsWithContext(ctx, &cloudwatchevents.RemoveTargetsInput{
						Rule: rule.Name,
						Ids:  ids,
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
					_, err = EventsClient().DeleteRuleWithContext(ctx, &cloudwatchevents.DeleteRuleInput{
						Name: rule.Name,
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
				}
				Logger.Println(PreviewString(preview)+"deleted cloudwatch rule:", name)
				break
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
					input.StartingPosition = aws.String(strings.ToUpper(value))
				default:
					err := fmt.Errorf("unknown lambda dynamodb trigger attribute: %s", line)
					Logger.Println("error:", err)
					return err
				}
			}
			if !preview {
				var expectedErr error
				err := Retry(ctx, func() error {
					_, err := LambdaClient().CreateEventSourceMappingWithContext(ctx, input)
					if err != nil {
						aerr, ok := err.(awserr.Error)
						if !ok || aerr.Code() != lambda.ErrCodeResourceConflictException {
							return err
						}
						expectedErr = err
						return nil
					}
					return err
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				if expectedErr == nil {
					Logger.Println(PreviewString(preview)+"created event source mapping:", name, arnLambda, streamArn, strings.Join(triggerAttrs, " "))
				} else {
					aerr, _ := expectedErr.(awserr.Error)
					parts := strings.Split(aerr.Message(), " ") // ... Please update or delete the existing mapping with UUID 50d4ed02-364f-4be6-97af-c6d3896fb262
					if parts[len(parts)-2] != "UUID" {
						return expectedErr
					}
					uuid := Last(parts)
					found, err := LambdaClient().GetEventSourceMappingWithContext(ctx, &lambda.GetEventSourceMappingInput{
						UUID: aws.String(uuid),
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
					input.StartingPosition = nil
					existing := map[string]*int64{
						"BatchSize":                      found.BatchSize,
						"MaximumBatchingWindowInSeconds": found.MaximumBatchingWindowInSeconds,
						"MaximumRetryAttempts":           found.MaximumRetryAttempts,
						"ParallelizationFactor":          found.ParallelizationFactor,
					}
					if input.MaximumBatchingWindowInSeconds == nil {
						input.MaximumBatchingWindowInSeconds = aws.Int64(0)
					}
					expected := map[string]*int64{
						"BatchSize":                      input.BatchSize,
						"MaximumBatchingWindowInSeconds": input.MaximumBatchingWindowInSeconds,
						"MaximumRetryAttempts":           input.MaximumRetryAttempts,
						"ParallelizationFactor":          input.ParallelizationFactor,
					}
					diff, err := diffMapStringInt64Pointers(existing, expected)
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
					if diff {
						if !preview {
							update := &lambda.UpdateEventSourceMappingInput{UUID: aws.String(uuid)}
							update.FunctionName = input.FunctionName
							update.BatchSize = input.BatchSize
							update.MaximumRetryAttempts = input.MaximumRetryAttempts
							update.ParallelizationFactor = input.ParallelizationFactor
							update.MaximumBatchingWindowInSeconds = input.MaximumBatchingWindowInSeconds
							_, err := LambdaClient().UpdateEventSourceMappingWithContext(ctx, update)
							if err != nil {
								Logger.Println("error:", err)
								return err
							}
						}
						Logger.Printf(PreviewString(preview)+"updated event source mapping for %s %s\n", name, tableName)
					}
				}
			}
		}
	}
	var marker *string
	for {
		out, err := LambdaClient().ListEventSourceMappingsWithContext(ctx, &lambda.ListEventSourceMappingsInput{
			FunctionName: aws.String(arnLambda),
			Marker:       marker,
		})
		if err != nil && !preview {
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

func lambdaListEventSourceMappings(ctx context.Context, name string) ([]*lambda.EventSourceMappingConfiguration, error) {
	var marker *string
	var eventSourceMappings []*lambda.EventSourceMappingConfiguration
	for {
		out, err := LambdaClient().ListEventSourceMappingsWithContext(ctx, &lambda.ListEventSourceMappingsInput{
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
			eventSourceMappings, err := lambdaListEventSourceMappings(ctx, name)
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
					Logger.Printf(PreviewString(preview)+"will update lambda event source mapping BatchSize for %s %s: %d => %d\n", name, queueName, *found.BatchSize, *input.BatchSize)
					update.BatchSize = input.BatchSize
					needsUpdate = true
				}
				if *found.MaximumRetryAttempts != *input.MaximumRetryAttempts {
					Logger.Printf(PreviewString(preview)+"will update lambda event source mapping MaximumRetryAttempts for %s %s: %d => %d\n", name, queueName, *found.MaximumRetryAttempts, *input.MaximumRetryAttempts)
					update.MaximumRetryAttempts = input.MaximumRetryAttempts
					needsUpdate = true
				}
				if *found.ParallelizationFactor != *input.ParallelizationFactor {
					Logger.Printf(PreviewString(preview)+"will update lambda event source mapping ParallelizationFactor for %s %s: %d => %d\n", name, queueName, *found.ParallelizationFactor, *input.ParallelizationFactor)
					update.ParallelizationFactor = input.ParallelizationFactor
					needsUpdate = true
				}
				if *found.MaximumBatchingWindowInSeconds != *input.MaximumBatchingWindowInSeconds {
					Logger.Printf(PreviewString(preview)+"will update lambda event source mapping MaximumBatchingWindowInSeconds for %s %s: %d => %d\n", name, queueName, *found.MaximumBatchingWindowInSeconds, *input.MaximumBatchingWindowInSeconds)
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
	var marker *string
	for {
		out, err := LambdaClient().ListEventSourceMappingsWithContext(ctx, &lambda.ListEventSourceMappingsInput{
			FunctionName: aws.String(arnLambda),
			Marker:       marker,
		})
		if err != nil && !preview {
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

func LambdaParseFile(path string) (*LambdaMetadata, error) {
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
	meta, err := LambdaGetMetadata(strings.Split(string(data), "\n"))
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

func lambdaUpdateZipGo(pth string, preview bool) error {
	return lambdaCreateZipGo(pth, []string{}, preview)
}

func lambdaCreateZipGo(pth string, _ []string, preview bool) error {
	zipFile, err := LambdaZipFile(pth)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	dir := path.Dir(zipFile)
	if !preview {
		err := os.RemoveAll(dir)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		_ = os.MkdirAll(dir, os.ModePerm)
	}
	Logger.Println(PreviewString(preview)+"created tempdir:", dir)
	if !preview {
		err = shellAt(path.Dir(pth), "CGO_ENABLED=0 GOOS=linux go build -ldflags='-s -w' -tags 'netgo osusergo' -o %s %s", path.Join(dir, "main"), path.Base(pth))
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = shellAt(dir, "zip -9 %s ./main", zipFile)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	Logger.Println(PreviewString(preview)+"zipped go binary:", zipFile)
	return nil
}

func lambdaCreateZipPy(pth string, requires []string, preview bool) error {
	zipFile, err := LambdaZipFile(pth)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	dir := path.Dir(zipFile)
	if !preview {
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
		err = shellAt(site_package, "zip -9 -r %s .", zipFile)
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
				pth, err := filepath.Rel(dir, pth)
				if err != nil {
					panic(err)
				}
				if !Exists(path.Join(dir, pth)) {
					continue
				}
				if !preview {
					err := shellAt(dir, "zip -9 -r %s '%s'", zipFile, pth)
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
				}
				Logger.Printf(PreviewString(preview)+"include in zip for %s: %s\n", name, pth)
			}
		} else {
			if !Exists(path.Join(dir, include)) {
				continue
			}
			if !preview {
				args := ""
				if strings.HasPrefix(include, "/") {
					args = "--junk-paths"
				}
				err := shellAt(dir, "zip -9 %s -r %s '%s'", args, zipFile, include)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Printf(PreviewString(preview)+"include in zip for %s: %s\n", name, include)
		}
	}
	return nil
}

func lambdaUpdateZipPy(pth string, preview bool) error {
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
		err = shellAt(site_package, "zip -9 %s %s", zipFile, path.Base(pth))
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	Logger.Println(PreviewString(preview)+"updated zip:", zipFile, pth)
	return nil
}

func LambdaListFunctions(ctx context.Context) ([]*lambda.FunctionConfiguration, error) {
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
		runtime := "python3.9"
		handler := strings.TrimSuffix(path.Base(pth), ".py") + ".main"
		return lambdaEnsure(ctx, runtime, handler, pth, quick, preview, lambdaUpdateZipPy, lambdaCreateZipPy)
	} else if strings.HasSuffix(pth, ".go") {
		runtime := "go1.x"
		handler := "main"
		return lambdaEnsure(ctx, runtime, handler, pth, quick, preview, lambdaUpdateZipGo, lambdaCreateZipGo)
	} else {
		return fmt.Errorf("lambda unknown file type: %s", pth)
	}
}

type LambdaUpdateZipFn func(pth string, preview bool) error

type LambdaCreateZipFn func(pth string, requires []string, preview bool) error

func lambdaEnsure(ctx context.Context, runtime, handler, pth string, quick, preview bool, updateZipFn LambdaUpdateZipFn, createZipFn LambdaCreateZipFn) error {
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
	metadata, err := LambdaParseFile(pth)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	concurrency := lambdaAttrConcurrencyDefault
	memory := lambdaAttrMemoryDefault
	timeout := lambdaAttrTimeoutDefault
	logsTTLDays := lambdaAttrLogsTTLDaysDefault
	for _, attr := range metadata.Attr {
		parts := strings.SplitN(attr, " ", 2)
		k := parts[0]
		v := parts[1]
		switch k {
		case lambdaAttrName:
			name = v
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
	zipFile, err := LambdaZipFile(pth)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if quick && (runtime != "python3.9" || Exists(zipFile)) { // python requires existing zip for quick
		if runtime == "go" {
			_ = os.Remove(zipFile) // go deletes existing zip on quick
		}
		err := updateZipFn(pth, preview)
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
	err = LogsEnsureGroup(ctx, "/aws/lambda/"+name, logsTTLDays, preview)
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
	err = createZipFn(pth, metadata.Require, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	err = LambdaIncludeInZip(pth, metadata.Include, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
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
		handler:  handler,
		runtime:  runtime,
		name:     name,
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
	err = LambdaSetConcurrency(ctx, name, concurrency, preview)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if !preview {
		err = Retry(ctx, func() error {
			_, err := LambdaClient().UpdateFunctionCodeWithContext(ctx, &lambda.UpdateFunctionCodeInput{
				FunctionName: aws.String(name),
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
	env := &lambda.Environment{Variables: make(map[string]*string)}
	for _, val := range metadata.Env {
		k, v, err := SplitOnce(val, "=")
		if err != nil {
			Logger.Fatal("error: ", err)
		}
		env.Variables[k] = aws.String(v)
	}
	out, err := LambdaClient().GetFunctionConfigurationWithContext(ctx, &lambda.GetFunctionConfigurationInput{
		FunctionName: aws.String(input.name),
	})
	if err != nil && !preview {
		Logger.Println("error:", err)
		return err
	}
	if out.Timeout == nil && preview {
		out = &lambda.FunctionConfiguration{
			Timeout:    aws.Int64(0),
			MemorySize: aws.Int64(0),
		}
	}
	if out.Environment == nil {
		out.Environment = &lambda.EnvironmentResponse{
			Variables: make(map[string]*string),
		}
	}
	needsUpdate := false
	logValues := false // because lambda env vars often contain secrets
	diff, err := diffMapStringStringPointers(env.Variables, out.Environment.Variables, logValues)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if diff {
		needsUpdate = true
		Logger.Println(PreviewString(preview) + "update env vars")
	}
	if *out.Timeout != int64(input.timeout) {
		needsUpdate = true
		Logger.Printf(PreviewString(preview)+"update timeout %d => %d\n", *out.Timeout, input.timeout)
	}
	if *out.MemorySize != int64(input.memory) {
		needsUpdate = true
		Logger.Printf(PreviewString(preview)+"update memory %d => %d\n", *out.MemorySize, input.memory)
	}
	if needsUpdate {
		if !preview {
			err = Retry(ctx, func() error {
				_, err = LambdaClient().UpdateFunctionConfigurationWithContext(ctx, &lambda.UpdateFunctionConfigurationInput{
					FunctionName: aws.String(input.name),
					Runtime:      aws.String(input.runtime),
					Timeout:      aws.Int64(int64(input.timeout)),
					MemorySize:   aws.Int64(int64(input.memory)),
					Handler:      aws.String(input.handler),
					Role:         aws.String(input.arnRole),
					Environment:  env,
				})
				return err
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"updated function configuration:", name)
	}
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
				Code:         &lambda.FunctionCode{ZipFile: input.zipBytes},
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

func LambdaDeleteFunction(ctx context.Context, name string, preview bool) error {
	if !preview {
		err := Retry(ctx, func() error {
			_, err := LambdaClient().DeleteFunctionWithContext(ctx, &lambda.DeleteFunctionInput{
				FunctionName: aws.String(name),
			})
			if err != nil {
				aerr, ok := err.(awserr.Error)
				if !ok || aerr.Code() != lambda.ErrCodeResourceNotFoundException {
					return err
				}
				return nil
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
