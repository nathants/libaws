package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
)

func handleRequest(_ context.Context, event json.RawMessage) (string, error) {
	fmt.Printf("event=%s\n", event)
	uid := os.Getenv("uid")
	fmt.Println(uid)
	return uid, nil
}

func main() {
	lambda.Start(handleRequest)
}
