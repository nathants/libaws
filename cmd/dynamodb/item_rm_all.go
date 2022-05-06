package cliaws

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["dynamodb-item-rm-all"] = dynamodbItemRmAll
	lib.Args["dynamodb-item-rm-all"] = dynamodbItemRmAllArgs{}
}

type dynamodbItemRmAllArgs struct {
	Table string   `arg:"positional"`
	Keys  []string `arg:"positional,required"`
}

func (dynamodbItemRmAllArgs) Description() string {
	return "\nrm all items in dynamodb table\n"
}

func dynamodbItemRmAll() {
	var args dynamodbItemRmAllArgs
	arg.MustParse(&args)
	ctx := context.Background()
	var start map[string]*dynamodb.AttributeValue
	for {
		out, err := lib.DynamoDBClient().ScanWithContext(ctx, &dynamodb.ScanInput{
			TableName:         aws.String(args.Table),
			ExclusiveStartKey: start,
			Limit:             aws.Int64(25),
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		reqs := []*dynamodb.WriteRequest{}
		for _, item := range out.Items {
			key := map[string]*dynamodb.AttributeValue{}
			for _, k := range args.Keys {
				key[k] = item[k]
			}
			reqs = append(reqs, &dynamodb.WriteRequest{
				DeleteRequest: &dynamodb.DeleteRequest{Key: key},
			})
		}
		input := &dynamodb.BatchWriteItemInput{
			RequestItems: map[string][]*dynamodb.WriteRequest{args.Table: reqs},
		}
		_, err = lib.DynamoDBClient().BatchWriteItemWithContext(ctx, input)
		if err != nil {
			if !strings.Contains(err.Error(), "[Member must have length less than or equal to 25, Member must have length greater than or equal to 1]") { // table already empty
				lib.Logger.Fatal("error: ", err)
			}
		}
		for _, req := range reqs {
			val := make(map[string]interface{})
			err := dynamodbattribute.UnmarshalMap(req.DeleteRequest.Key, &val)
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}
			bytes, err := json.Marshal(val)
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}
			fmt.Println(string(bytes))
		}
		if out.LastEvaluatedKey == nil {
			break
		}
		start = out.LastEvaluatedKey
	}
}
