package main

import (
	"context"
	"errors"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handleRequest(_ context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	switch request.Path {
	case "/backend-fail":
		return events.APIGatewayProxyResponse{Body: "backend unavailable", StatusCode: 503}, nil
	case "/integration-fail":
		return events.APIGatewayProxyResponse{}, errors.New("access log integration fixture failure")
	default:
		return events.APIGatewayProxyResponse{Body: "hi\n", StatusCode: 200}, nil
	}
}

func main() {
	lambda.Start(handleRequest)
}
