package lib

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

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

// DynamoDBMarshalItemJSON marshals an item without losing DynamoDB number precision.
func DynamoDBMarshalItemJSON(item map[string]ddbtypes.AttributeValue) ([]byte, error) {
	value := map[string]any{}
	if err := attributevalue.UnmarshalMapWithOptions(item, &value, func(options *attributevalue.DecoderOptions) {
		options.UseNumber = true
	}); err != nil {
		return nil, err
	}
	return json.Marshal(exactDynamoDBJSONValue(value))
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
			data, err := DynamoDBMarshalItemJSON(item)
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

// DynamoDBItemScan writes scanned table items as JSON lines.
func DynamoDBItemScan(ctx context.Context, table string, limit, pageSize int, output io.Writer) error {
	return dynamoDBItemScanWithClient(ctx, DynamoDBClient(), table, limit, pageSize, output)
}
