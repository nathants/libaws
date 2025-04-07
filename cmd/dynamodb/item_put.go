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
	item := map[string]ddbtypes.AttributeValue{}

	for _, key := range args.Attr {
		name, kind, val, err := lib.SplitTwice(key, ":")
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		switch strings.ToUpper(kind) {
		case "S":
			item[name] = &ddbtypes.AttributeValueMemberS{
				Value: val,
			}
		case "B":
			item[name] = &ddbtypes.AttributeValueMemberBOOL{
				Value: val == "true",
			}
		case "N":
			item[name] = &ddbtypes.AttributeValueMemberN{
				Value: val,
			}
		default:
			panic(kind)
		}
	}

	_, err := lib.DynamoDBClient().PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(args.Table),
		Item:      item,
	})

	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
