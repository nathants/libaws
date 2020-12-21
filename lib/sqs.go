package lib

import (
	"context"
	"sync"

	"github.com/aws/aws-sdk-go/service/sqs"
)

var sqsClient *sqs.SQS
var sqsClientLock sync.RWMutex

func SQSClient() *sqs.SQS {
	sqsClientLock.Lock()
	defer sqsClientLock.Unlock()
	if sqsClient == nil {
		sqsClient = sqs.New(Session())
	}
	return sqsClient
}

func SqsListQueues(ctx context.Context) ([]*string, error) {
	Logger.Println("list queues",)
	var nextToken *string
	var queues []*string
	for {
		out, err := SQSClient().ListQueuesWithContext(ctx, &sqs.ListQueuesInput{
			NextToken: nextToken,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		queues = append(queues, out.QueueUrls...)
		if out.NextToken == nil {
			break
		}
		nextToken = out.NextToken
	}
	return queues, nil
}
