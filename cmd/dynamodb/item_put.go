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
	lib.Commands["dynamodb-item-put"] = dynamodbItemPut
	lib.Args["dynamodb-item-put"] = dynamodbItemPutArgs{}
}

type dynamodbItemPutArgs struct {
	Table string   `arg:"positional,required"`
	Keys  []string `arg:"positional,required"`
}

func (dynamodbItemPutArgs) Description() string {
	return `

put item
describe vals like: $name:s|n|b:$value

>> cli-aws dynamodb-item-put test-table user:s:john

`
}

func dynamodbItemPut() {
	var args dynamodbItemPutArgs
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
	_, err := lib.DynamoDBClient().PutItemWithContext(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(args.Table),
		Item:      item,
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
