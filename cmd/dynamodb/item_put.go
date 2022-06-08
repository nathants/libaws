package cliaws

import (
	"context"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["dynamodb-item-put"] = dynamodbItemPut
	lib.Args["dynamodb-item-put"] = dynamodbItemPutArgs{}
}

type dynamodbItemPutArgs struct {
	Table string   `arg:"positional,required"`
	Attr  []string `arg:"positional,required"`
}

func (dynamodbItemPutArgs) Description() string {
	return `

put item

describe attributes like: $name:s|n|b:$value

>> libaws dynamodb-item-put test-table user:s:jane dob:n:1984

`
}

func dynamodbItemPut() {
	var args dynamodbItemPutArgs
	arg.MustParse(&args)
	ctx := context.Background()
	item := map[string]*dynamodb.AttributeValue{}
	for _, key := range args.Attr {
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
