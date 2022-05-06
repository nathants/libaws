package main

import (
	"context"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handleRequest(_ context.Context, _ events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	// TODO
	//
	// this test currently just checks that the websocket trigger can be successfully deployed.
	//
	// for websocket example see: https://github.com/nathants/new-gocljs
	//
	return events.APIGatewayProxyResponse{StatusCode: 200}, nil
}

func main() {
	lambda.Start(handleRequest)
}
