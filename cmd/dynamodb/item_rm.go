package libaws

import (
	"context"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/nathants/libaws/lib"
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

>> libaws dynamodb-item-rm test-table user:s:john

`
}

func dynamodbItemRm() {
	var args dynamodbItemRmArgs
	arg.MustParse(&args)
	ctx := context.Background()
	item := map[string]ddbtypes.AttributeValue{}
	for _, key := range args.Keys {
		name, kind, val, err := lib.SplitTwice(key, ":")
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		switch strings.ToUpper(kind) {
		case "S":
			item[name] = &ddbtypes.AttributeValueMemberS{Value: val}
		case "N":
			item[name] = &ddbtypes.AttributeValueMemberN{Value: val}
		default:
			panic(kind)
		}
	}
	_, err := lib.DynamoDBClient().DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(args.Table),
		Key:       item,
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
