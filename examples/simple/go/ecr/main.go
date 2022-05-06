package main

import (
	"context"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/nathants/libaws/lib"
)

func handleRequest(_ context.Context, event interface{}) (events.APIGatewayProxyResponse, error) {
	lib.Logger.Println(lib.Pformat(event))
	return events.APIGatewayProxyResponse{StatusCode: 200}, nil
}

func main() {
	lambda.Start(handleRequest)
}
