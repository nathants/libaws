package cliaws

import (
	"context"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["dynamodb-item-rm"] = dynamodbItemRm
	lib.Args["dynamodb-item-rm"] = dynamodbItemRmArgs{}
}

type dynamodbItemRmArgs struct {
	Table string   `arg:"positional,required"`
	Keys  []string `arg:"positional,required"`
}

func (dynamodbItemRmArgs) Description() string {
	return `

delete item
describe keys like: $name:s|n:$value

>> aws-dynamodb-delete test-table user:s:john

`
}

func dynamodbItemRm() {
	var args dynamodbItemRmArgs
	arg.MustParse(&args)
	ctx := context.Background()
	item := map[string]*dynamodb.AttributeValue{}
	for _, key := range args.Keys {
		name, kind, val, err := lib.SplitTwice(key, ":")
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		switch strings.ToUpper(kind) {
		case "S":
			item[name] = &dynamodb.AttributeValue{S: aws.String(val)}
		case "N":
			item[name] = &dynamodb.AttributeValue{N: aws.String(val)}
		default:
			panic(kind)
		}
	}
	_, err := lib.DynamoDBClient().DeleteItemWithContext(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(args.Table),
		Key:       item,
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
