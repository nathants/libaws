package main

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handleRequest(_ context.Context, _ events.SimpleEmailEvent) (string, error) {
	uid := os.Getenv("uid")
	fmt.Println(uid)
	return uid, nil
}

func main() {
	lambda.Start(handleRequest)
}
