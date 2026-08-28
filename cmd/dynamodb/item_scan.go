package libaws

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
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

func validateDynamoDBItemScanLimit(limit int) error {
	if limit < 0 {
		return fmt.Errorf("scan limit must not be negative: %d", limit)
	}
	return nil
}

func dynamoDBItemScanLimitReached(limit, count int) bool {
	return limit > 0 && count >= limit
}

func dynamodbItemScan() {
	var args dynamodbItemScanArgs
	arg.MustParse(&args)
	if err := validateDynamoDBItemScanLimit(args.Limit); err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	ctx := context.Background()
	var start map[string]ddbtypes.AttributeValue
	count := 0
	for {
		out, err := lib.DynamoDBClient().Scan(ctx, &dynamodb.ScanInput{
			TableName:         aws.String(args.Table),
			ExclusiveStartKey: start,
			Limit:             aws.Int32(1000),
		})
		if err != nil {
			panic(err)
		}
		for _, item := range out.Items {
			if dynamoDBItemScanLimitReached(args.Limit, count) {
				return
			}
			count++
			val := map[string]any{}
			err = attributevalue.UnmarshalMap(item, &val)
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
