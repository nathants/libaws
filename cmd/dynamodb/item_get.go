package cliaws

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["dynamodb-item-get"] = dynamodbItemGet
	lib.Args["dynamodb-item-get"] = dynamodbItemGetArgs{}
}

type dynamodbItemGetArgs struct {
	Table string   `arg:"positional,required"`
	Keys  []string `arg:"positional,required"`
}

func (dynamodbItemGetArgs) Description() string {
	return `

get item
describe keys like: $name:s|n:$value

>> libaws dynamodb-item-get test-table user:s:john

`
}

func dynamodbItemGet() {
	var args dynamodbItemGetArgs
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
	out, err := lib.DynamoDBClient().GetItemWithContext(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(args.Table),
		Key:       item,
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	if out.Item == nil {
		os.Exit(1)
	}
	val := make(map[string]interface{})
	err = dynamodbattribute.UnmarshalMap(out.Item, &val)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	bytes, err := json.Marshal(val)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(string(bytes))
}
