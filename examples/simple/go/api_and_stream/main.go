package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func streamHandler(_ context.Context, _ *events.LambdaFunctionURLRequest) (*events.LambdaFunctionURLStreamingResponse, error) {
	pr, pw := io.Pipe()
	go func() {
		defer func() { _ = pw.Close() }()
		for _, v := range []string{"a\n", "b\n", "c\n", "d\n"} {
			_, _ = pw.Write([]byte(v))
			time.Sleep(2 * time.Second)
		}
	}()
	return &events.LambdaFunctionURLStreamingResponse{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type": "text/plain",
		},
		Body: pr,
	}, nil
}

func dispatch(ctx context.Context, raw json.RawMessage) (any, error) {
	var urlReq events.LambdaFunctionURLRequest
	if json.Unmarshal(raw, &urlReq) == nil && urlReq.RequestContext.HTTP.Method != "" {
		return streamHandler(ctx, &urlReq)
	}
	var apiReq events.APIGatewayProxyRequest
	if json.Unmarshal(raw, &apiReq) == nil && apiReq.HTTPMethod != "" {
		return events.APIGatewayProxyResponse{StatusCode: 200, Body: "ok\n"}, nil
	}
	return nil, fmt.Errorf("unknown event")
}

func main() {
	lambda.Start(dispatch)
}
