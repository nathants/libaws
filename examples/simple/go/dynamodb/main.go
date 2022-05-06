package main

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
	"github.com/nathants/libaws/lib"
)

type RecordKey struct {
	UserID  string `json:"userid"`
	Version int    `json:"version"`
}

type RecordData struct {
	Data string `json:"data"`
}

type Record struct {
	RecordKey
	RecordData
}

var uid = os.Getenv("uid")

func handleRequest(ctx context.Context, event events.DynamoDBEvent) (events.APIGatewayProxyResponse, error) {
	for _, record := range event.Records {
		userid := record.Change.Keys["userid"].String()
		version, err := record.Change.Keys["version"].Integer()
		if err != nil {
			lib.Logger.Println("error:", err)
			return events.APIGatewayProxyResponse{StatusCode: 500, Body: err.Error()}, nil
		}
		key := RecordKey{
			UserID:  userid,
			Version: int(version),
		}
		item, err := dynamodbattribute.MarshalMap(key)
		if err != nil {
			lib.Logger.Println("error:", err)
			return events.APIGatewayProxyResponse{StatusCode: 500, Body: err.Error()}, nil
		}
		out, err := lib.DynamoDBClient().GetItemWithContext(ctx, &dynamodb.GetItemInput{
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
		_, err = lib.DynamoDBClient().PutItemWithContext(ctx, &dynamodb.PutItemInput{
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
