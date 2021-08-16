package lib

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
)

type Infra struct {
	Account  []string
	Api      []string
	DynamoDB []string
	EC2      []string
}

func InfraListApis(ctx context.Context) ([]string, error) {
	infraApi := make([]string, 0)
	apis, err := apiList(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	for _, api := range apis {
		infraApi = append(infraApi, *api.Name)
	}
	return infraApi, nil
}

func InfraListDynamoDBs(ctx context.Context) ([]string, error) {
	infraDynamoDB := make([]string, 0)
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
		var table []string
		table = append(table, tableName)
		attrTypes := make(map[string]string)
		for _, attr := range out.Table.AttributeDefinitions {
			attrTypes[*attr.AttributeName] = *attr.AttributeType
		}
		for _, key := range out.Table.KeySchema {
			table = append(table, fmt.Sprintf("%s:%s:%s", *key.AttributeName, attrTypes[*key.AttributeName], *key.KeyType))
		}
		table = append(table, fmt.Sprintf("ProvisionedThroughput.ReadCapacityUnits=%d", *out.Table.ProvisionedThroughput.ReadCapacityUnits))
		table = append(table, fmt.Sprintf("ProvisionedThroughput.WriteCapacityUnits=%d", *out.Table.ProvisionedThroughput.WriteCapacityUnits))
		if out.Table.StreamSpecification != nil {
			table = append(table, fmt.Sprintf("StreamSpecification.StreamViewType=%s", *out.Table.StreamSpecification.StreamViewType))
		}
		for i, index := range out.Table.LocalSecondaryIndexes {
			table = append(table, fmt.Sprintf("LocalSecondaryIndexes.%d.IndexName=%s", i, *index.IndexName))
			for j, key := range index.KeySchema {
				table = append(table, fmt.Sprintf("LocalSecondaryIndexes.%d.Key.%d=%s:%s:%s", i, j, *key.AttributeName, attrTypes[*key.AttributeName], *key.KeyType))
			}
			table = append(table, fmt.Sprintf("LocalSecondaryIndexes.%d.Projection.ProjectionType=%s", i, *index.Projection.ProjectionType))
			for j, attr := range index.Projection.NonKeyAttributes {
				table = append(table, fmt.Sprintf("LocalSecondaryIndexes.%d.Projection.NonKeyAttributes.%d=%s", i, j, *attr))
			}
		}
		for i, index := range out.Table.GlobalSecondaryIndexes {
			table = append(table, fmt.Sprintf("GlobalSecondaryIndexes.%d.IndexName=%s", i, *index.IndexName))
			for j, key := range index.KeySchema {
				table = append(table, fmt.Sprintf("GlobalSecondaryIndexes.%d.Key.%d=%s:%s:%s", i, j, *key.AttributeName, attrTypes[*key.AttributeName], *key.KeyType))
			}
			table = append(table, fmt.Sprintf("GlobalSecondaryIndexes.%d.Projection.ProjectionType=%s", i, *index.Projection.ProjectionType))
			for j, attr := range index.Projection.NonKeyAttributes {
				table = append(table, fmt.Sprintf("GlobalSecondaryIndexes.%d.Projection.NonKeyAttributes.%d=%s", i, j, *attr))
			}
			table = append(table, fmt.Sprintf("GlobalSecondaryIndexes.%d.ProvisionedThroughput.ReadCapacityUnits=%d", i, *index.ProvisionedThroughput.ReadCapacityUnits))
			table = append(table, fmt.Sprintf("GlobalSecondaryIndexes.%d.ProvisionedThroughput.WriteCapacityUnits=%d", i, *index.ProvisionedThroughput.WriteCapacityUnits))
		}
		tags, err := DynamoDBListTags(ctx, tableName)
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		for i, tag := range tags {
			table = append(table, fmt.Sprintf("Tags.%d.Key=%s", i, *tag.Key))
			table = append(table, fmt.Sprintf("Tags.%d.Value=%s", i, *tag.Value))
		}
		infraDynamoDB = append(infraDynamoDB, strings.Join(table, " "))
	}
	return infraDynamoDB, nil
}

func InfraListEC2s(ctx context.Context) ([]string, error) {
	infraEC2 := make([]string, 0)
	instances, err := EC2ListInstances(ctx, nil, "running")
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	for _, instance := range instances {
		var ec2 []string
		ec2 = append(ec2, EC2Name(instance.Tags))
		ec2 = append(ec2, fmt.Sprintf("Type=%s", *instance.InstanceType))
		ec2 = append(ec2, fmt.Sprintf("Image=%s", *instance.ImageId))
		ec2 = append(ec2, fmt.Sprintf("Kind=%s", EC2Kind(instance)))
		ec2 = append(ec2, fmt.Sprintf("Vpc=%s", *instance.VpcId))
		for _, tag := range instance.Tags {
			if *tag.Key != "creation-date" && *tag.Key != "Name" {
				ec2 = append(ec2, fmt.Sprintf("Tags.%s=%s", *tag.Key, *tag.Value))
			}
		}
		infraEC2 = append(infraEC2, strings.Join(ec2, " "))
	}
	return infraEC2, nil
}

func InfraList(ctx context.Context) (*Infra, error) {
	var err error
	infra := Infra{}
	account, err := StsAccount(ctx)
	if err != nil {
		Logger.Fatal("error: ", err)
	}
	infra.Account = append(infra.Account, account)
	infra.Api, err = InfraListApis(ctx)
	if err != nil {
		Logger.Fatal("error: ", err)
	}
	infra.DynamoDB, err = InfraListDynamoDBs(ctx)
	if err != nil {
		Logger.Fatal("error: ", err)
	}
	infra.EC2, err = InfraListEC2s(ctx)
	if err != nil {
		Logger.Fatal("error: ", err)
	}

	return &infra, nil
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
