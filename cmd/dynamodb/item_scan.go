package cliaws

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["dynamodb-item-scan"] = dynamodbItemScan
	lib.Args["dynamodb-item-scan"] = dynamodbItemScanArgs{}
}

type dynamodbItemScanArgs struct {
	Table string `arg:"positional"`
	Limit int    `arg:"-l,--limit" default:"0"`
}

func (dynamodbItemScanArgs) Description() string {
	return "\nscan dynamodb table\n"
}

func dynamodbItemScan() {
	var args dynamodbItemScanArgs
	arg.MustParse(&args)
	ctx := context.Background()
	var start map[string]*dynamodb.AttributeValue
	count := 0
	for {
		out, err := lib.DynamoDBClient().ScanWithContext(ctx, &dynamodb.ScanInput{
			TableName:         aws.String(args.Table),
			ExclusiveStartKey: start,
			Limit:             aws.Int64(1000),
		})
		if err != nil {
			panic(err)
		}
		for _, item := range out.Items {
			if args.Limit != 0 && args.Limit < count {
				break
			}
			count++
			val := make(map[string]interface{})
			err := dynamodbattribute.UnmarshalMap(item, &val)
			if err != nil {
				panic(err)
			}
			bytes, err := json.Marshal(val)
			if err != nil {
				panic(err)
			}
			fmt.Println(string(bytes))
		}
		if out.LastEvaluatedKey == nil {
			break
		}
		start = out.LastEvaluatedKey
	}
}
