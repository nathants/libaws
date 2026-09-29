package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/nathants/libaws/lib"
)

var uid = os.Getenv("uid")
var logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))

func recordFailure(record events.DynamoDBEventRecord, operation string, err error) error {
	logger.Error("dynamodb record failed", "event_id", record.EventID, "source_arn", record.EventSourceArn, "operation", operation, "error", err.Error())
	return fmt.Errorf("%s for record %s: %w", operation, record.EventID, err)
}

func handleRequest(ctx context.Context, event events.DynamoDBEvent) error {
	for _, record := range event.Records {
		keys, err := lib.FromDynamoDBEventAVMap(record.Change.Keys)
		if err != nil {
			return recordFailure(record, "decode keys", err)
		}
		out, err := lib.DynamoDBClient().GetItem(ctx, &dynamodb.GetItemInput{
			TableName:      aws.String("test-table-" + uid),
			Key:            keys,
			ConsistentRead: aws.Bool(true),
		})
		if err != nil {
			return recordFailure(record, "GetItem", err)
		}
		if len(out.Item) == 0 {
			return recordFailure(record, "GetItem", errors.New("source item no longer exists"))
		}
		_, err = lib.DynamoDBClient().PutItem(ctx, &dynamodb.PutItemInput{
			TableName: aws.String("test-other-table-" + uid),
			Item:      out.Item,
		})
		if err != nil {
			return recordFailure(record, "PutItem", err)
		}
		logger.Info("dynamodb record copied", "event_id", record.EventID)
	}
	return nil
}

func main() {
	lambda.Start(handleRequest)
}
