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
	"github.com/aws/aws-sdk-go/service/acm"
	"github.com/aws/aws-sdk-go/service/apigateway"
	"github.com/aws/aws-sdk-go/service/cloudwatchevents"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/aws/aws-sdk-go/service/route53"
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
	out, err := LambdaClient().GetFunctionConcurrencyWithContext(ctx, &lambda.GetFunctionConcurrencyInput{
		FunctionName: aws.String(name),
	})
	if err != nil {
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
			line = strings.Split(line, "#")[0]
			line = strings.Split(line, "//")[0]
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

func lambdaParseMetadata(token string, lines []string) ([]string, error) {
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

func LambdaGetMetadata(lines []string) (*LambdaMetadata, error) {
	var err error
	meta := &LambdaMetadata{}
	meta.S3, err = lambdaParseMetadata(lambdaMetaS3, lines)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.DynamoDB, err = lambdaParseMetadata(lambdaMetaDynamoDB, lines)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Sqs, err = lambdaParseMetadata(lambdaMetaSQS, lines)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Policy, err = lambdaParseMetadata(lambdaMetaPolicy, lines)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Allow, err = lambdaParseMetadata(lambdaMetaAllow, lines)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Include, err = lambdaParseMetadata(lambdaMetaInclude, lines)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Trigger, err = lambdaParseMetadata(lambdaMetaTrigger, lines)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Require, err = lambdaParseMetadata(lambdaMetaRequire, lines)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Attr, err = lambdaParseMetadata(lambdaMetaAttr, lines)
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
		if strings.Trim(line, " ") == "" {
			continue
		}
		token := strings.SplitN(line, ":", 2)[0]
		if strings.HasPrefix(line, "- ") || !Contains([]string{lambdaMetaS3, lambdaMetaDynamoDB, lambdaMetaSQS, lambdaMetaPolicy, lambdaMetaAllow, lambdaMetaInclude, lambdaMetaTrigger, lambdaMetaRequire, lambdaMetaAttr}, token) {
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
				Logger.Printf(PreviewString(preview)+"updated bucket notifications for %s %s: %s => %s\n", bucket, name, existingEvents, events)
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

func lambdaEnsureTriggerApiRestApi(ctx context.Context, name string, preview bool) (*apigateway.RestApi, error) {
	restApi, err := Api(ctx, name)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
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
				return nil, err
			}
			return out, nil
		}
		Logger.Println(PreviewString(preview)+"created rest api:", name)
		return nil, nil
	}
	if !reflect.DeepEqual(restApi.BinaryMediaTypes, apiBinaryMediaTypes) {
		err := fmt.Errorf("api binary media types misconfigured for %s %s: %v != %v", name, *restApi.Id, StringSlice(restApi.BinaryMediaTypes), StringSlice(apiBinaryMediaTypes))
		Logger.Println("error:", err)
		return nil, err
	}
	if !reflect.DeepEqual(restApi.EndpointConfiguration.Types, apiEndpointConfigurationTypes) {
		err := fmt.Errorf("api endpoint configuration types misconfigured for %s %s: %v != %v", name, *restApi.Id, StringSlice(restApi.EndpointConfiguration.Types), StringSlice(apiEndpointConfigurationTypes))
		Logger.Println("error:", err)
		return nil, err
	}
	return restApi, nil
}

func lambdaEnsureTriggerApiDeployment(ctx context.Context, name string, restApi *apigateway.RestApi, preview bool) error {
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
			Logger.Println(PreviewString(preview)+"created api resource:", name)
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
				Logger.Println(PreviewString(preview)+"created api method:", name)
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
				Logger.Println(PreviewString(preview)+"created api integration:", name)
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
			Logger.Println(PreviewString(preview)+"created deployment:", name)
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
	return nil
}

func lambdaEnsureTriggerApiDomainName(ctx context.Context, name, subDomain, parentDomain string, preview bool) error {
	out, err := ApiClient().GetDomainNameWithContext(ctx, &apigateway.GetDomainNameInput{
		DomainName: aws.String(subDomain),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != apigateway.ErrCodeNotFoundException {
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
			if *cert.DomainName == parentDomain {
				arnCert = *cert.CertificateArn
				break
			}
		}
		if arnCert == "" {
			err := fmt.Errorf("no acm cert found for: %s", parentDomain)
			Logger.Println("error:", err)
			return err
		}
		if subDomain != parentDomain {
			out, err := AcmClient().DescribeCertificateWithContext(ctx, &acm.DescribeCertificateInput{
				CertificateArn: aws.String(arnCert),
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			wildcard := fmt.Sprintf("*.%s", parentDomain)
			if !Contains(StringSlice(out.Certificate.SubjectAlternativeNames), wildcard) {
				err := fmt.Errorf("no wildcard domain for parent domain: %s %s", wildcard, subDomain)
				Logger.Println("error:", err)
				return err
			}
		}
		if !preview {
			_, err = ApiClient().CreateDomainNameWithContext(ctx, &apigateway.CreateDomainNameInput{
				RegionalCertificateArn: aws.String(arnCert),
				DomainName:             aws.String(subDomain),
				EndpointConfiguration: &apigateway.EndpointConfiguration{
					Types: []*string{aws.String(apigateway.EndpointTypeRegional)},
				},
				SecurityPolicy: aws.String(apigateway.SecurityPolicyTls12),
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"created api domain:", name, subDomain)
	} else {
		if len(out.EndpointConfiguration.Types) != 1 || *out.EndpointConfiguration.Types[0] != apigateway.EndpointTypeRegional {
			err := fmt.Errorf("api endpoint type misconfigured: %s", Pformat(out.EndpointConfiguration))
			Logger.Println("error:", err)
			return err
		}
		if out.SecurityPolicy == nil || *out.SecurityPolicy != apigateway.SecurityPolicyTls12 {
			err := fmt.Errorf("api security policy misconfigured: %s", Pformat(out.SecurityPolicy))
			Logger.Println("error:", err)
			return err
		}
	}
	return nil
}

func lambdaEnsureTriggerApiDnsRecords(ctx context.Context, name, subDomain string, zone *route53.HostedZone, preview bool) error {
	out, err := ApiClient().GetDomainNameWithContext(ctx, &apigateway.GetDomainNameInput{
		DomainName: aws.String(subDomain),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != apigateway.ErrCodeNotFoundException {
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
				if strings.TrimRight(*record.AliasTarget.DNSName, ".") != *out.RegionalDomainName {
					err := fmt.Errorf("alias target misconfigured: %s != %s", *record.AliasTarget.DNSName, *out.RegionalDomainName)
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
									DNSName:              out.RegionalDomainName,
									HostedZoneId:         out.RegionalHostedZoneId,
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
			Logger.Println(PreviewString(preview)+"created api dns:", name, subDomain, *out.RegionalDomainName)
		}
	}
	return nil
}

func lambdaEnsureTriggerApiDns(ctx context.Context, name, domain string, restApi *apigateway.RestApi, preview bool) error {
	zones, err := Route53ListZones(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	found := false
	for _, zone := range zones {
		if domain == strings.TrimRight(*zone.Name, ".") {
			found = true
			err := lambdaEnsureTriggerApiDomainName(ctx, name, domain, domain, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			err = lambdaEnsureTriggerApiDnsRecords(ctx, name, domain, zone, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			err = lambdaEnsureTriggerApiBasePathMapping(ctx, name, domain, restApi, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			break
		}
	}
	//
	if !found {
		_, parentDomain, err := splitOnce(domain, ".")
		subDomain := domain
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		for _, zone := range zones {
			if parentDomain == strings.TrimRight(*zone.Name, ".") {
				found = true
				err := lambdaEnsureTriggerApiDomainName(ctx, name, subDomain, parentDomain, preview)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				err = lambdaEnsureTriggerApiDnsRecords(ctx, name, subDomain, zone, preview)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				err = lambdaEnsureTriggerApiBasePathMapping(ctx, name, subDomain, restApi, preview)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				break
			}
		}
	}
	if !found {
		err = fmt.Errorf("no zone found matching: %s", domain)
		Logger.Println("error:", err)
		return err
	}
	return nil
}

func lambdaEnsureTriggerApiBasePathMapping(ctx context.Context, name, subDomain string, restApi *apigateway.RestApi, preview bool) error {
	mappings, err := ApiClient().GetBasePathMappingsWithContext(ctx, &apigateway.GetBasePathMappingsInput{
		DomainName: aws.String(subDomain),
		Limit:      aws.Int64(500),
	})
	if err != nil || len(mappings.Items) == 500 {
		Logger.Println("error:", err)
		return err
	}
	switch len(mappings.Items) {
	case 0:
		if !preview {
			_, err := ApiClient().CreateBasePathMappingWithContext(ctx, &apigateway.CreateBasePathMappingInput{
				BasePath:   aws.String(apiMappingBasePath),
				DomainName: aws.String(subDomain),
				RestApiId:  restApi.Id,
				Stage:      aws.String(apiStageName),
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"created api path mapping:", name, subDomain, *restApi.Id)
	case 1:
		mapping := mappings.Items[0]
		if *mapping.BasePath != apiMappingBasePathEmpty {
			err := fmt.Errorf("base path misconfigured: %s != %s", *mapping.BasePath, apiMappingBasePath)
			Logger.Println("error:", err)
			return err
		}
		if *mapping.RestApiId != *restApi.Id {
			err := fmt.Errorf("restapi id misconfigured: %s != %s", *mapping.RestApiId, *restApi.Id)
			Logger.Println("error:", err)
			return err
		}
		if *mapping.Stage != apiStageName {
			err := fmt.Errorf("stage misconfigured: %s != %s", *mapping.Stage, apiStageName)
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
	domainName := ""
	for _, trigger := range meta.Trigger {
		parts := strings.Split(trigger, " ")
		kind := parts[0]
		attrs := parts[1:]
		if kind == lambdaTriggerApi {
			hasApi = true
			restApi, err := lambdaEnsureTriggerApiRestApi(ctx, name, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			err = lambdaEnsureTriggerApiDeployment(ctx, name, restApi, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			for _, attr := range attrs {
				k, v, err := splitOnce(attr, "=")
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				switch k {
				case "dns":
					domainName = v
					err := lambdaEnsureTriggerApiDns(ctx, name, domainName, restApi, preview)
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
				default:
					err := fmt.Errorf("unknown attr: %s", attr)
					Logger.Println("error:", err)
					return err
				}
			}
			break
		}
	}
	//
	api, err := Api(ctx, name)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if api != nil {
		domains, err := ApiListDomains(ctx)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		// delete any unused domains
		for _, domain := range domains {
			if *domain.DomainName == domainName {
				continue
			}
			err := lambdaTriggerApiDeleteDns(ctx, name, api, domain, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		// if api trigger unused, delete rest api
		if !hasApi {
			err = lambdaTriggerApiDeleteRestApi(ctx, name, api, preview)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
	}
	//
	return nil
}

func lambdaTriggerApiDeleteRestApi(ctx context.Context, name string, api *apigateway.RestApi, preview bool) error {
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
	return nil
}

func lambdaTriggerApiDeleteDns(ctx context.Context, name string, api *apigateway.RestApi, domain *apigateway.DomainName, preview bool) error {
	mappings, err := ApiClient().GetBasePathMappingsWithContext(ctx, &apigateway.GetBasePathMappingsInput{
		DomainName: domain.DomainName,
		Limit:      aws.Int64(500),
	})
	if err != nil || len(mappings.Items) == 500 {
		Logger.Println("error:", err)
		return err
	}
	for _, mapping := range mappings.Items {
		if *mapping.RestApiId == *api.Id {
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
					if record.AliasTarget != nil && record.AliasTarget.DNSName != nil && strings.TrimRight(*record.AliasTarget.DNSName, ".") == *domain.RegionalDomainName {
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
						Logger.Println(PreviewString(preview)+"deleted api dns records:", name, *zone.Name, *zone.Id)
					}
				}
			}
			//
			if !preview {
				_, err := ApiClient().DeleteDomainNameWithContext(ctx, &apigateway.DeleteDomainNameInput{
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
					input.StartingPosition = aws.String(strings.ToUpper(value))
				default:
					err := fmt.Errorf("unknown lambda dynamodb trigger attribute: %s", line)
					Logger.Println("error:", err)
					return err
				}
			}
			//
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
					//
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
		err = shellAt(dir, "zip %s ./main", zipFile)
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
	} else if strings.Contains(strings.ToLower(path.Base(pth)), "dockerfile") {
		return lambdaEnsureDockerfile(ctx, pth, quick, preview)
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
	zipFile, err := LambdaZipFile(pth)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if quick && (runtime != "python3.9" || Exists(zipFile)) {
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
	//
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
	//
	out, err := LambdaClient().GetFunctionConfigurationWithContext(ctx, &lambda.GetFunctionConfigurationInput{
		FunctionName: aws.String(input.name),
	})
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	//
	if out.Environment == nil {
		out.Environment = &lambda.EnvironmentResponse{
			Variables: make(map[string]*string),
		}
	}
	needsUpdate := false
	diff, err := diffMapStringStringPointers(env.Variables, out.Environment.Variables)
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
	//
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

func lambdaEnsureDockerfile(ctx context.Context, pth string, quick, preview bool) error {
	_ = ctx
	_ = pth
	_ = quick
	_ = preview
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
