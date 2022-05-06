package main

import (
	"context"
	"fmt"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handleRequest(_ context.Context, event events.S3Event) (events.APIGatewayProxyResponse, error) {
	for _, record := range event.Records {
		fmt.Println(record.S3.Object.Key)
	}
	return events.APIGatewayProxyResponse{StatusCode: 200}, nil
}

func main() {
	lambda.Start(handleRequest)
}
