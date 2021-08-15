package lib

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
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
	out, err := SQSClient().GetQueueUrlWithContext(ctx, &sqs.GetQueueUrlInput{
		QueueName: aws.String(queueName),
	})
	if err != nil {
		return "", err
	}
	return *out.QueueUrl, nil
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

type sqsEnsureInput struct {
	name                          string
	delaySeconds                  int
	maximumMessageSize            int
	messageRetentionPeriod        int
	receiveMessageWaitTimeSeconds int
	visibilityTimeout             int
}

func (input *sqsEnsureInput) Attrs() map[string]*string {
	m := make(map[string]*string)
	if input.delaySeconds != -1 {
		m["DelaySeconds"] = aws.String(fmt.Sprint(input.delaySeconds))
	}
	if input.maximumMessageSize != -1 {
		m["MaximumMessageSize"] = aws.String(fmt.Sprint(input.maximumMessageSize))
	}
	if input.messageRetentionPeriod != -1 {
		m["MessageRetentionPeriod"] = aws.String(fmt.Sprint(input.messageRetentionPeriod))
	}
	if input.receiveMessageWaitTimeSeconds != -1 {
		m["ReceiveMessageWaitTimeSeconds"] = aws.String(fmt.Sprint(input.receiveMessageWaitTimeSeconds))
	}
	if input.visibilityTimeout != -1 {
		m["VisibilityTimeout"] = aws.String(fmt.Sprint(input.visibilityTimeout))
	}
	if len(m) != 0 {
		return m
	}
	return nil
}

func SQSEnsureInput(name string, attrs []string) (*sqsEnsureInput, error) {
	input := &sqsEnsureInput{
		name:                          name,
		delaySeconds:                  -1,
		maximumMessageSize:            -1,
		messageRetentionPeriod:        -1,
		receiveMessageWaitTimeSeconds: -1,
		visibilityTimeout:             -1,
	}
	for _, line := range attrs {
		line = strings.ToLower(line)
		attr, value, err := splitOnce(line, "=")
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		switch attr {
		case "delayseconds":
			num, err := strconv.Atoi(value)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			input.delaySeconds = num
		case "maximummessagesize":
			num, err := strconv.Atoi(value)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			input.maximumMessageSize = num
		case "messageretentionperiod":
			num, err := strconv.Atoi(value)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			input.messageRetentionPeriod = num
		case "receivemessagewaittimeseconds":
			num, err := strconv.Atoi(value)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			input.receiveMessageWaitTimeSeconds = num
		case "visibilitytimeout":
			num, err := strconv.Atoi(value)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			input.visibilityTimeout = num
		default:
			err := fmt.Errorf("unknown sqs attr: %s", line)
			Logger.Println("error:", err)
			return nil, err
		}
	}
	return input, nil
}

func SQSEnsure(ctx context.Context, input *sqsEnsureInput, preview bool) error {
	sqsUrl, err := SQSQueueUrl(ctx, input.name)
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != sqs.ErrCodeQueueDoesNotExist {
			Logger.Println("error:", err)
			return err
		}
		if !preview {
			_, err := SQSClient().CreateQueueWithContext(ctx, &sqs.CreateQueueInput{
				QueueName:  aws.String(input.name),
				Attributes: input.Attrs(),
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Printf(PreviewString(preview)+"sqs created queue: %#v\n", *input)
		for k, v := range input.Attrs() {
			if v != nil {
				Logger.Println(PreviewString(preview)+"sqs created attribute:", input.name, k, *v)
			}
		}

	} else {
		attrsOut, err := SQSClient().GetQueueAttributesWithContext(ctx, &sqs.GetQueueAttributesInput{
			QueueUrl: aws.String(sqsUrl),
			AttributeNames: []*string{
				aws.String("DelaySeconds"),
				aws.String("MaximumMessageSize"),
				aws.String("MessageRetentionPeriod"),
				aws.String("ReceiveMessageWaitTimeSeconds"),
				aws.String("VisibilityTimeout"),
			},
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		needsUpdate := false
		attrs := attrsOut.Attributes
		if input.delaySeconds != -1 && attrs["DelaySeconds"] != nil && input.delaySeconds != atoi(*attrs["DelaySeconds"]) {
			needsUpdate = true
		}
		if input.maximumMessageSize != -1 && attrs["MaximumMessageSize"] != nil && input.maximumMessageSize != atoi(*attrs["MaximumMessageSize"]) {
			needsUpdate = true
		}
		if input.messageRetentionPeriod != -1 && attrs["MessageRetentionPeriod"] != nil && input.messageRetentionPeriod != atoi(*attrs["MessageRetentionPeriod"]) {
			needsUpdate = true
		}
		if input.receiveMessageWaitTimeSeconds != -1 && attrs["ReceiveMessageWaitTimeSeconds"] != nil && input.receiveMessageWaitTimeSeconds != atoi(*attrs["ReceiveMessageWaitTimeSeconds"]) {
			needsUpdate = true
		}
		if input.visibilityTimeout != -1 && attrs["VisibilityTimeout"] != nil && input.visibilityTimeout != atoi(*attrs["VisibilityTimeout"]) {
			needsUpdate = true
		}
		if needsUpdate {
			if !preview {
				_, err := SQSClient().SetQueueAttributesWithContext(ctx, &sqs.SetQueueAttributesInput{
					QueueUrl:   aws.String(sqsUrl),
					Attributes: input.Attrs(),
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Printf(PreviewString(preview)+"sqs updated queue: %#v\n", *input)
		}
	}
	return nil
}

func SQSDeleteQueue(ctx context.Context, name string, preview bool) error {
	url, err := SQSQueueUrl(ctx, name)
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != sqs.ErrCodeQueueDoesNotExist {
			return err
		}
		return nil
	}
	if !preview {
		_, err := SQSClient().DeleteQueueWithContext(ctx, &sqs.DeleteQueueInput{
			QueueUrl: aws.String(url),
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	Logger.Println(PreviewString(preview)+"sqs deleted queue:", name)
	return nil
}
