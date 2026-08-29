package libaws

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"

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
	Table    string `arg:"positional,required"`
	Limit    int    `arg:"-l,--limit" default:"0" help:"maximum items to print; zero is unlimited"`
	PageSize int    `arg:"--page-size" default:"1000" help:"maximum items evaluated per request"`
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

func validateDynamoDBItemScanPageSize(pageSize int) error {
	if pageSize <= 0 || pageSize > math.MaxInt32 {
		return fmt.Errorf("scan page size must be between 1 and %d: %d", math.MaxInt32, pageSize)
	}
	return nil
}

func dynamoDBItemScanRequestLimit(limit, count, pageSize int) int32 {
	requestLimit := pageSize
	if limit > 0 && limit-count < requestLimit {
		requestLimit = limit - count
	}
	return int32(requestLimit)
}

func dynamoDBItemScanLimitReached(limit, count int) bool {
	return limit > 0 && count >= limit
}

func exactDynamoDBJSONValue(value any) any {
	switch value := value.(type) {
	case attributevalue.Number:
		return json.Number(value.String())
	case []attributevalue.Number:
		exact := make([]any, len(value))
		for index := range value {
			exact[index] = json.Number(value[index].String())
		}
		return exact
	case []any:
		for index := range value {
			value[index] = exactDynamoDBJSONValue(value[index])
		}
		return value
	case map[string]any:
		for key := range value {
			value[key] = exactDynamoDBJSONValue(value[key])
		}
		return value
	default:
		return value
	}
}

type dynamoDBItemScanClient interface {
	Scan(
		context.Context,
		*dynamodb.ScanInput,
		...func(*dynamodb.Options),
	) (*dynamodb.ScanOutput, error)
}

func dynamoDBItemScanWithClient(
	ctx context.Context,
	client dynamoDBItemScanClient,
	table string,
	limit int,
	pageSize int,
	output io.Writer,
) error {
	if err := validateDynamoDBItemScanLimit(limit); err != nil {
		return err
	}
	if err := validateDynamoDBItemScanPageSize(pageSize); err != nil {
		return err
	}
	var start map[string]ddbtypes.AttributeValue
	count := 0
	for {
		if dynamoDBItemScanLimitReached(limit, count) {
			return nil
		}
		out, err := client.Scan(ctx, &dynamodb.ScanInput{
			TableName:         aws.String(table),
			ExclusiveStartKey: start,
			Limit:             aws.Int32(dynamoDBItemScanRequestLimit(limit, count, pageSize)),
		})
		if err != nil {
			return err
		}
		for _, item := range out.Items {
			if dynamoDBItemScanLimitReached(limit, count) {
				return nil
			}
			value := map[string]any{}
			if err := attributevalue.UnmarshalMapWithOptions(item, &value, func(options *attributevalue.DecoderOptions) {
				options.UseNumber = true
			}); err != nil {
				return err
			}
			data, err := json.Marshal(exactDynamoDBJSONValue(value))
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintln(output, string(data)); err != nil {
				return err
			}
			count++
		}
		if len(out.LastEvaluatedKey) == 0 {
			return nil
		}
		start = out.LastEvaluatedKey
	}
}

func dynamodbItemScan() {
	var args dynamodbItemScanArgs
	arg.MustParse(&args)
	if err := dynamoDBItemScanWithClient(
		context.Background(),
		lib.DynamoDBClient(),
		args.Table,
		args.Limit,
		args.PageSize,
		os.Stdout,
	); err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
