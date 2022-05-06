package main

import (
	"context"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/nathants/libaws/lib"
)

func handleRequest(_ context.Context, _ interface{}) (interface{}, error) {
	data, err := os.ReadFile("include_me.txt")
	if err != nil {
		lib.Logger.Println("error:", err)
		return nil, err
	}
	return strings.TrimRight(string(data), "\n"), nil
}

func main() {
	lambda.Start(handleRequest)
}
