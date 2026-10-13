package main

import (
	"context"
	"fmt"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

// Deliberately exhaust retries to demonstrate retention and its two alarms.
func handleRequest(_ context.Context, event events.DynamoDBEvent) error {
	return fmt.Errorf("demonstration failure processing %d records", len(event.Records))
}

func main() {
	lambda.Start(handleRequest)
}
