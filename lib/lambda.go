package lib

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"reflect"
	"regexp"

	"strings"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/aws/aws-sdk-go/service/s3"
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
			Logger.Printf(PreviewString(preview)+"updated concurrency for %s: %d", name, concurrency)
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
	if !silent && len(results) > 0 {
		Logger.Println(token)
		for _, result := range results {
			Logger.Println("", result)
		}
	}
	return results, nil
}

type LambdaMetadata struct {
	S3       []string
	DynamoDB []string
	Sns      []string
	Sqs      []string
	Policy   []string
	Allow    []string
	Include  []string
	Trigger  []string
	Require  []string
	Conf     []string
}

func LambdaGetMetadata(lines []string, silent bool) (*LambdaMetadata, error) {
	var err error
	meta := &LambdaMetadata{}
	meta.S3, err = lambdaParseMetadata("s3:", lines, silent)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.DynamoDB, err = lambdaParseMetadata("dynamodb:", lines, silent)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Sns, err = lambdaParseMetadata("sns:", lines, silent)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Sqs, err = lambdaParseMetadata("sqs:", lines, silent)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Policy, err = lambdaParseMetadata("policy:", lines, silent)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Allow, err = lambdaParseMetadata("allow:", lines, silent)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Include, err = lambdaParseMetadata("include:", lines, silent)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Trigger, err = lambdaParseMetadata("trigger:", lines, silent)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Require, err = lambdaParseMetadata("require:", lines, silent)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	meta.Conf, err = lambdaParseMetadata("conf:", lines, silent)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	for _, conf := range meta.Conf {
		parts := strings.SplitN(conf, " ", 2)
		k := parts[0]
		v := parts[1]
		if !Contains([]string{"concurrency", "memory", "timeout"}, k) {
			err := fmt.Errorf("unknown conf: %s", k)
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
		if !Contains([]string{"sns", "sqs", "s3", "dynamodb", "api", "cloudwatch"}, trigger) {
			err := fmt.Errorf("unknown trigger: %s", trigger)
			Logger.Println("error:", err)
			return nil, err
		}
	}
	for _, line := range lambdaFilterMetadata(lines) {
		token := strings.SplitN(line, ":", 2)[0]
		if !Contains([]string{"s3", "dynamodb", "sns", "sqs", "policy", "allow", "include", "trigger", "require", "conf"}, token) {
			err := fmt.Errorf("unknown configuration comment: %s", line)
			Logger.Println("error:", err)
			return nil, err
		}
	}
	return meta, nil
}

func LambdaEnsureTriggerS3(ctx context.Context, name, arnLambda string, meta LambdaMetadata, preview bool) error {
	events := []*string{aws.String("s3:ObjectCreated:*"), aws.String("s3:ObjectRemoved:*")}
	var triggers []string
	for _, trigger := range meta.Trigger {
		parts := strings.Split(trigger, " ")
		kind := parts[0]
		if kind == "s3" {
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
				Logger.Println(PreviewString(preview)+"updated bucket notifications for %s %s: %s => %s", bucket, name, existingEvents, events)
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
	sid := strings.ReplaceAll(callerPrincipal, ".", "-")
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

func LambdaEnsureTriggerApi(ctx context.Context, name, arnLambda string, meta LambdaMetadata, preview bool) error {
	// for _, trigger := range meta.Trigger {
	// 	parts := strings.Split(trigger, " ")
	// 	kind := parts[0]
	// 	if kind == "api" {
	// 		break
	// 	}
	// }
	return fmt.Errorf("unimplemented")
}

func LambdaEnsureTriggerCloudwatch(ctx context.Context, name, arnLambda string, meta LambdaMetadata, preview bool) error {
	return fmt.Errorf("unimplemented")
}

func LambdaEnsureTriggerSns(ctx context.Context, name, arnLambda string, meta LambdaMetadata, preview bool) error {
	return fmt.Errorf("unimplemented")
}

func LambdaEnsureTriggerDynamoDB(ctx context.Context, name, arnLambda string, meta LambdaMetadata, preview bool) error {
	return fmt.Errorf("unimplemented")
}

func LambdaEnsureTriggerSqs(ctx context.Context, name, arnLambda string, meta LambdaMetadata, preview bool) error {
	return fmt.Errorf("unimplemented")
}
