package lib

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
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

func SQSListQueues(ctx context.Context) ([]*string, error) {
	Logger.Println("list queues")
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

func atoi(a string) int {
	i, err := strconv.Atoi(a)
	if err != nil {
		panic(err)
	}
	return i
}

func SQSQueueUrl(ctx context.Context, queueName string) (string, error) {
	account, err := Account(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("https://sqs.%s.amazonaws.com/%s/%s", Region(), account, queueName), nil
}

type SQSNumMessageOutput struct {
	Messages           int
	MessagesNotVisible int
	MessagesDelayed    int
}

func SQSNumMessages(ctx context.Context, queueUrl string) (*SQSNumMessageOutput, error) {
	out, err := SQSClient().GetQueueAttributesWithContext(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl: aws.String(queueUrl),
		AttributeNames: []*string{
			aws.String(sqs.QueueAttributeNameApproximateNumberOfMessages),
			aws.String(sqs.QueueAttributeNameApproximateNumberOfMessagesNotVisible),
			aws.String(sqs.QueueAttributeNameApproximateNumberOfMessagesDelayed),
		},
	})
	if err != nil {
		return nil, err
	}
	return &SQSNumMessageOutput{
		atoi(*out.Attributes[sqs.QueueAttributeNameApproximateNumberOfMessages]),
		atoi(*out.Attributes[sqs.QueueAttributeNameApproximateNumberOfMessagesNotVisible]),
		atoi(*out.Attributes[sqs.QueueAttributeNameApproximateNumberOfMessagesDelayed]),
	}, nil
}
