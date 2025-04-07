package libaws

import (
	"context"
	"fmt"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["dynamodb-describe"] = dynamodbDescribe
	lib.Args["dynamodb-describe"] = dynamodbDescribeArgs{}
}

type dynamodbDescribeArgs struct {
	Table string `arg:"positional,required" help:"table name"`
}

func (dynamodbDescribeArgs) Description() string {
	return "\ndescribe dynamodb table\n"
}

func dynamodbDescribe() {
	var args dynamodbDescribeArgs
	arg.MustParse(&args)
	ctx := context.Background()
	out, err := lib.DynamoDBClient().DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(args.Table),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	attrs := map[string]ddbtypes.ScalarAttributeType{}
	for _, attr := range out.Table.AttributeDefinitions {
		attrs[*attr.AttributeName] = attr.AttributeType
	}
	for _, key := range out.Table.KeySchema {
		vals := []string{
			*key.AttributeName,
			string(attrs[*key.AttributeName]),
			string(key.KeyType),
		}
		fmt.Println(strings.ToLower(strings.Join(vals, ":")))
	}
}
