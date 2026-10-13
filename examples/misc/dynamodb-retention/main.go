package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

// Backlogged records succeed; other records exhaust retries.
func handleRequest(_ context.Context, event events.DynamoDBEvent) error {
	if len(event.Records) == 1 && strings.HasPrefix(event.Records[0].Change.Keys["id"].String(), "backlog-") {
		fmt.Println("processed", event.Records[0].Change.Keys["id"].String())
		return nil
	}
	return fmt.Errorf("demonstration failure processing %d records", len(event.Records))
}

func main() {
	lambda.Start(handleRequest)
}
