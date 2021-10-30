//
// attr: concurrency 0
// attr: memory 128
// attr: timeout 60
// policy: AWSLambdaBasicExecutionRole
// trigger: api
// trigger: cloudwatch rate(15 minutes) # keep lambda warm for fast responses

package main

import (
	"context"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handleRequest(_ context.Context, _ events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	return events.APIGatewayProxyResponse{Body: "hi!?\n", StatusCode: 200}, nil
}

func main() {
	lambda.Start(handleRequest)
}
