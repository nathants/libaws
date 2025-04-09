package main

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/nathants/libaws/lib"
)

type RecordKey struct {
	UserID  string `json:"userid"  dynamodbav:"userid"`
	Version int    `json:"version" dynamodbav:"version"`
}

type RecordData struct {
	Data string `json:"data" dynamodbav:"data"`
}

type Record struct {
	RecordKey
	RecordData
}

var uid = os.Getenv("uid")

func handleRequest(ctx context.Context, event events.DynamoDBEvent) (events.APIGatewayProxyResponse, error) {
	for _, record := range event.Records {
		item, err := lib.FromDynamoDBEventAVMap(record.Change.Keys)
		if err != nil {
			lib.Logger.Println("error:", err)
			return events.APIGatewayProxyResponse{StatusCode: 500, Body: err.Error()}, nil
		}
		out, err := lib.DynamoDBClient().GetItem(ctx, &dynamodb.GetItemInput{
			TableName: aws.String("test-table-" + uid),
			Key:       item,
		})
		if err != nil {
			lib.Logger.Println("error:", err)
			return events.APIGatewayProxyResponse{StatusCode: 500, Body: err.Error()}, nil
		}
		if out.Item == nil {
			lib.Logger.Println("error:", err)
			return events.APIGatewayProxyResponse{StatusCode: 404}, nil
		}
		_, err = lib.DynamoDBClient().PutItem(ctx, &dynamodb.PutItemInput{
			TableName: aws.String("test-other-table-" + uid),
			Item:      out.Item,
		})
		if err != nil {
			lib.Logger.Println("error:", err)
			return events.APIGatewayProxyResponse{StatusCode: 500, Body: err.Error()}, nil
		}
		fmt.Printf("put: %#v\n", out.Item)
	}
	return events.APIGatewayProxyResponse{StatusCode: 200}, nil
}

func main() {
	lambda.Start(handleRequest)
}
