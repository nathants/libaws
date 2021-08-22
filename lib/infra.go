package lib

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/apigateway"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/aws/aws-sdk-go/service/sqs"
)

type Infra struct {
	Account  map[string]string
	Api      map[string]string
	DynamoDB map[string]string
	EC2      map[string]string
	SQS      map[string]string
	Lambda   map[string]string
	S3       map[string]string
}

type infraLambdaTrigger struct {
	lambdaName   string
	triggerType  string
	triggerAttrs []string
}

func InfraList(ctx context.Context) (*Infra, error) {
	var err error
	infra := Infra{}
	account, err := StsAccount(ctx)
	if err != nil {
		Logger.Fatal("error: ", err)
	}
	infra.Account = map[string]string{account: ""}
	errs := make(chan error)
	count := 0
	triggersChan := make(chan infraLambdaTrigger, 1024)
	//
	run := func(fn func()) {
		go fn()
		count++
	}
	//
	run(func() {
		infra.Api, err = InfraListApi(ctx, triggersChan)
		errs <- err
	})
	//
	run(func() {
		infra.DynamoDB, err = InfraListDynamoDB(ctx, triggersChan)
		errs <- err
	})
	//
	run(func() {
		infra.EC2, err = InfraListEC2(ctx)
		errs <- err
	})
	//
	run(func() {
		infra.SQS, err = InfraListSQS(ctx, triggersChan)
		errs <- err
	})
	//
	run(func() {
		infra.S3, err = InfraListS3(ctx, triggersChan)
		errs <- err
	})
	//
	run(func() {
		_, err = InfraListCloudwatch(ctx, triggersChan)
		errs <- err
	})
	//
	lambdaErr := make(chan error)
	go func() {
		infra.Lambda, err = InfraListLambda(ctx, triggersChan)
		lambdaErr <- err
	}()
	//
	for i := 0; i < count; i++ {
		err := <-errs
		if err != nil {
			Logger.Fatal("error: ", err)
		}
	}
	close(triggersChan)
	//
	err = <-lambdaErr
	if err != nil {
		Logger.Fatal("error: ", err)
	}
	//
	return &infra, nil
}

func InfraListCloudwatch(ctx context.Context, triggersChan chan<- infraLambdaTrigger) (map[string]string, error) {
	rules, err := EventsListRules(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	for _, rule := range rules {
		targets, err := EventsListRuleTargets(ctx, *rule.Name)
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		for _, target := range targets {
			if strings.HasPrefix(*target.Arn, "arn:aws:lambda:") {
				triggersChan <- infraLambdaTrigger{
					lambdaName:   Last(strings.Split(*target.Arn, ":")),
					triggerType:  lambdaTriggerCloudwatch,
					triggerAttrs: []string{*rule.ScheduleExpression},
				}
			}
		}
	}
	return nil, nil
}

func InfraListLambda(ctx context.Context, triggersChan <-chan infraLambdaTrigger) (map[string]string, error) {
	fns, err := LambdaListFunctions(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	triggers := make(map[string][]infraLambdaTrigger)
	for trigger := range triggersChan {
		triggers[trigger.lambdaName] = append(triggers[trigger.lambdaName], trigger)
	}
	res := make(map[string]string)
	for _, fn := range fns {
		parts := []string{}
		if *fn.MemorySize != 128 { // default
			parts = append(parts, fmt.Sprintf("conf=memory::%d", *fn.MemorySize))
		}
		if *fn.Timeout != 3 { // default
			parts = append(parts, fmt.Sprintf("conf=timeout::%d", *fn.Timeout))
		}
		out, err := LambdaClient().GetFunctionConcurrencyWithContext(ctx, &lambda.GetFunctionConcurrencyInput{
			FunctionName: aws.String(*fn.FunctionName),
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		if out.ReservedConcurrentExecutions != nil {
			parts = append(parts, fmt.Sprintf("conf=concurrency::%d", *out.ReservedConcurrentExecutions))
		}
		ts, ok := triggers[*fn.FunctionName]
		if ok {
			for _, trigger := range ts {
				val := fmt.Sprintf("trigger=%s", trigger.triggerType)
				if len(trigger.triggerAttrs) > 0 {
					val += "::" + strings.ReplaceAll(strings.Join(trigger.triggerAttrs, "::"), " ", "::")
				}
				parts = append(parts, val)
			}
		}
		//
		roleName := Last(strings.Split(*fn.Role, "/"))
		//
		policies, err := IamListRolePolicies(ctx, roleName)
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		for _, policy := range policies {
			parts = append(parts, fmt.Sprintf("policy=%s", *policy.PolicyName))
		}
		//
		allows, err := IamListRoleAllows(ctx, roleName)
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		for _, allow := range allows {
			parts = append(parts, strings.ReplaceAll(fmt.Sprintf("allow=%s", allow), " ", "::"))
		}
		//
		res[*fn.FunctionName] = strings.Join(parts, " ")
	}
	return res, nil
}

func InfraListApi(ctx context.Context, triggersChan chan<- infraLambdaTrigger) (map[string]string, error) {
	infraApi := make(map[string]string)
	apis, err := apiList(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	for _, api := range apis {
		infraApi[*api.Name] = ""
		parentID, err := ApiResourceID(ctx, *api.Id, "/")
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		out, err := ApiClient().GetIntegrationWithContext(ctx, &apigateway.GetIntegrationInput{
			RestApiId:  api.Id,
			HttpMethod: aws.String(apiHttpMethod),
			ResourceId: aws.String(parentID),
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		lambdaName := LambdaApiUriToLambdaName(*out.Uri)
		triggersChan <- infraLambdaTrigger{
			lambdaName:  lambdaName,
			triggerType: lambdaTriggerApi,
		}
	}
	return infraApi, nil
}

func InfraListDynamoDB(ctx context.Context, triggersChan chan<- infraLambdaTrigger) (map[string]string, error) {
	infraDynamoDB := make(map[string]string)
	tableNames, err := DynamoDBListTables(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	for _, tableName := range tableNames {
		out, err := DynamoDBClient().DescribeTableWithContext(ctx, &dynamodb.DescribeTableInput{
			TableName: aws.String(tableName),
		})
		if err != nil {
			Logger.Fatal("error: ", err)
		}
		var parts []string
		attrTypes := make(map[string]string)
		for _, attr := range out.Table.AttributeDefinitions {
			attrTypes[*attr.AttributeName] = *attr.AttributeType
		}
		for _, key := range out.Table.KeySchema {
			parts = append(parts, fmt.Sprintf("%s:%s:%s", *key.AttributeName, attrTypes[*key.AttributeName], *key.KeyType))
		}
		parts = append(parts, fmt.Sprintf("ProvisionedThroughput.ReadCapacityUnits=%d", *out.Table.ProvisionedThroughput.ReadCapacityUnits))
		parts = append(parts, fmt.Sprintf("ProvisionedThroughput.WriteCapacityUnits=%d", *out.Table.ProvisionedThroughput.WriteCapacityUnits))
		if out.Table.StreamSpecification != nil {
			parts = append(parts, fmt.Sprintf("StreamSpecification.StreamViewType=%s", *out.Table.StreamSpecification.StreamViewType))
		}
		for i, index := range out.Table.LocalSecondaryIndexes {
			parts = append(parts, fmt.Sprintf("LocalSecondaryIndexes.%d.IndexName=%s", i, *index.IndexName))
			for j, key := range index.KeySchema {
				parts = append(parts, fmt.Sprintf("LocalSecondaryIndexes.%d.Key.%d=%s:%s:%s", i, j, *key.AttributeName, attrTypes[*key.AttributeName], *key.KeyType))
			}
			parts = append(parts, fmt.Sprintf("LocalSecondaryIndexes.%d.Projection.ProjectionType=%s", i, *index.Projection.ProjectionType))
			for j, attr := range index.Projection.NonKeyAttributes {
				parts = append(parts, fmt.Sprintf("LocalSecondaryIndexes.%d.Projection.NonKeyAttributes.%d=%s", i, j, *attr))
			}
		}
		for i, index := range out.Table.GlobalSecondaryIndexes {
			parts = append(parts, fmt.Sprintf("GlobalSecondaryIndexes.%d.IndexName=%s", i, *index.IndexName))
			for j, key := range index.KeySchema {
				parts = append(parts, fmt.Sprintf("GlobalSecondaryIndexes.%d.Key.%d=%s:%s:%s", i, j, *key.AttributeName, attrTypes[*key.AttributeName], *key.KeyType))
			}
			parts = append(parts, fmt.Sprintf("GlobalSecondaryIndexes.%d.Projection.ProjectionType=%s", i, *index.Projection.ProjectionType))
			for j, attr := range index.Projection.NonKeyAttributes {
				parts = append(parts, fmt.Sprintf("GlobalSecondaryIndexes.%d.Projection.NonKeyAttributes.%d=%s", i, j, *attr))
			}
			parts = append(parts, fmt.Sprintf("GlobalSecondaryIndexes.%d.ProvisionedThroughput.ReadCapacityUnits=%d", i, *index.ProvisionedThroughput.ReadCapacityUnits))
			parts = append(parts, fmt.Sprintf("GlobalSecondaryIndexes.%d.ProvisionedThroughput.WriteCapacityUnits=%d", i, *index.ProvisionedThroughput.WriteCapacityUnits))
		}
		tags, err := DynamoDBListTags(ctx, tableName)
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		for i, tag := range tags {
			parts = append(parts, fmt.Sprintf("Tags.%d.Key=%s", i, *tag.Key))
			parts = append(parts, fmt.Sprintf("Tags.%d.Value=%s", i, *tag.Value))
		}
		infraDynamoDB[tableName] = strings.Join(parts, " ")
	}
	return infraDynamoDB, nil
}

func InfraListEC2(ctx context.Context) (map[string]string, error) {
	infraEC2 := make(map[string]string)
	instances, err := EC2ListInstances(ctx, nil, "running")
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	for _, instance := range instances {
		var ec2 []string
		ec2 = append(ec2, fmt.Sprintf("Type=%s", *instance.InstanceType))
		ec2 = append(ec2, fmt.Sprintf("Image=%s", *instance.ImageId))
		ec2 = append(ec2, fmt.Sprintf("Kind=%s", EC2Kind(instance)))
		ec2 = append(ec2, fmt.Sprintf("Vpc=%s", *instance.VpcId))
		for _, tag := range instance.Tags {
			if *tag.Key != "creation-date" && *tag.Key != "Name" {
				ec2 = append(ec2, fmt.Sprintf("Tags.%s=%s", *tag.Key, *tag.Value))
			}
		}
		infraEC2[EC2Name(instance.Tags)] = strings.Join(ec2, " ")
	}
	return infraEC2, nil
}

func InfraListS3(ctx context.Context, triggersChan chan<- infraLambdaTrigger) (map[string]string, error) {
	res := make(map[string]string)
	return res, nil
}

func InfraListSQS(ctx context.Context, triggersChan chan<- infraLambdaTrigger) (map[string]string, error) {
	urls, err := SQSListQueueUrls(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	res := make(map[string]string)
	for _, url := range urls {
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		out, err := SQSClient().GetQueueAttributesWithContext(ctx, &sqs.GetQueueAttributesInput{
			QueueUrl: aws.String(url),
			AttributeNames: []*string{
				aws.String("DelaySeconds"),
				aws.String("MaximumMessageSize"),
				aws.String("MessageRetentionPeriod"),
				aws.String("ReceiveMessageWaitTimeSeconds"),
				aws.String("VisibilityTimeout"),
				aws.String("KmsDataKeyReusePeriodSeconds"),
			},
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		parts := []string{}
		if *out.Attributes["DelaySeconds"] != "0" { // default
			parts = append(parts, "DelaySeconds="+*out.Attributes["DelaySeconds"])
		}
		if *out.Attributes["MaximumMessageSize"] != "262144" { // default
			parts = append(parts, "MaximumMessageSize="+*out.Attributes["MaximumMessageSize"])
		}
		if *out.Attributes["MessageRetentionPeriod"] != "345600" { // default
			parts = append(parts, "MessageRetentionPeriod="+*out.Attributes["MessageRetentionPeriod"])
		}
		if *out.Attributes["ReceiveMessageWaitTimeSeconds"] != "0" { // default
			parts = append(parts, "ReceiveMessageWaitTimeSeconds="+*out.Attributes["ReceiveMessageWaitTimeSeconds"])
		}
		if *out.Attributes["VisibilityTimeout"] != "30" {
			parts = append(parts, "VisibilityTimeout="+*out.Attributes["VisibilityTimeout"])
		}
		if *out.Attributes["KmsDataKeyReusePeriodSeconds"] != "300" {
			parts = append(parts, "KmsDataKeyReusePeriodSeconds="+*out.Attributes["KmsDataKeyReusePeriodSeconds"])
		}
		res[SQSUrlToName(url)] = strings.Join(parts, " ")
	}
	return res, nil
}

func InfraEnsureS3(ctx context.Context, buckets []string, preview bool) error {
	for _, bucket := range buckets {
		parts := strings.Split(bucket, " ")
		name := parts[0]
		attrs := parts[1:]
		input, err := S3EnsureInput(name, attrs)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = S3Ensure(ctx, input, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	return nil
}

func InfraEnsureDynamoDB(ctx context.Context, dbs []string, preview bool) error {
	for _, db := range dbs {
		parts := strings.Split(db, " ")
		name := parts[0]
		var keys []string
		var attrs []string
		for _, part := range parts[1:] {
			if strings.Contains(part, "=") {
				attrs = append(attrs, part)
			} else {
				keys = append(keys, part)
			}
		}
		input, err := DynamoDBEnsureInput(name, keys, attrs)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = DynamoDBEnsure(ctx, input, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	return nil
}

func InfraEnsureSqs(ctx context.Context, queues []string, preview bool) error {
	for _, queue := range queues {
		parts := strings.Split(queue, "/")
		name := parts[0]
		attrs := parts[1:]
		input, err := SQSEnsureInput(name, attrs)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = SQSEnsure(ctx, input, preview)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	return nil
}
