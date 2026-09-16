package main

import (
	"context"

	"github.com/aws/aws-lambda-go/lambda"
)

func handleRequest(context.Context) (string, error) {
	return "lambda-build-fixture", nil
}

func main() {
	lambda.Start(handleRequest)
}
