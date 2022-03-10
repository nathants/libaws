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

func SQSClientClear() {
	sqsClientLock.Lock()
	defer sqsClientLock.Unlock()
	sqsClient = nil
	sessionClear()
}

func SQSClient() *sqs.SQS {
	sqsClientLock.Lock()
	defer sqsClientLock.Unlock()
	if sqsClient == nil {
		sqsClient = sqs.New(Session())
	}
	return sqsClient
}

func SQSListQueues(ctx context.Context) ([]string, error) {
	Logger.Println("list queues")
	var nextToken *string
	var queues []string
	for {
		out, err := SQSClient().ListQueuesWithContext(ctx, &sqs.ListQueuesInput{
			NextToken: nextToken,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		for _, queue := range out.QueueUrls {
			queues = append(queues, Last(strings.Split(*queue, "/")))
		}
		if out.NextToken == nil {
			break
		}
		nextToken = out.NextToken
	}
	return queues, nil
}

func SQSUrlToName(url string) string {
	return Last(strings.Split(url, "/"))
}

func SQSListQueueUrls(ctx context.Context) ([]string, error) {
	var nextToken *string
	var queues []string
	for {
		out, err := SQSClient().ListQueuesWithContext(ctx, &sqs.ListQueuesInput{
			NextToken: nextToken,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		for _, queue := range out.QueueUrls {
			queues = append(queues, *queue)
		}
		if out.NextToken == nil {
			break
		}
		nextToken = out.NextToken
	}
	return queues, nil
}

func Atoi(a string) int {
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

func SQSArn(ctx context.Context, name string) (string, error) {
	account, err := StsAccount(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	return fmt.Sprintf("arn:aws:sqs:%s:%s:%s", Region(), account, name), nil
}

func SQSArnToName(arn string) string {
	return Last(strings.Split(arn, ":"))
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
		Atoi(*out.Attributes[sqs.QueueAttributeNameApproximateNumberOfMessages]),
		Atoi(*out.Attributes[sqs.QueueAttributeNameApproximateNumberOfMessagesNotVisible]),
		Atoi(*out.Attributes[sqs.QueueAttributeNameApproximateNumberOfMessagesDelayed]),
	}, nil
}

type sqsEnsureInput struct {
	name                          string
	delaySeconds                  int
	maximumMessageSize            int
	messageRetentionPeriod        int
	receiveMessageWaitTimeSeconds int
	visibilityTimeout             int
	kmsDataKeyReusePeriodSeconds  int
}

func (input *sqsEnsureInput) Attrs() map[string]*string {
	m := make(map[string]*string)
	m["KmsMasterKeyId"] = aws.String("alias/aws/sqs")
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
	if input.kmsDataKeyReusePeriodSeconds != -1 {
		m["KmsDataKeyReusePeriodSeconds"] = aws.String(fmt.Sprint(input.kmsDataKeyReusePeriodSeconds))
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
		kmsDataKeyReusePeriodSeconds:  -1,
	}
	for _, line := range attrs {
		line = strings.ToLower(line)
		attr, value, err := SplitOnce(line, "=")
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
		case "kmsdatakeyreuseperiodseconds":
			num, err := strconv.Atoi(value)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			input.kmsDataKeyReusePeriodSeconds = num
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
				aws.String("KmsDataKeyReusePeriodSeconds"),
			},
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		needsUpdate := false
		attrs := attrsOut.Attributes
		if input.delaySeconds != -1 && attrs["DelaySeconds"] != nil && input.delaySeconds != Atoi(*attrs["DelaySeconds"]) {
			Logger.Printf(PreviewString(preview)+"sqs will update attr %s for %s: %d => %d\n", "DelaySeconds", input.name, Atoi(*attrs["DelaySeconds"]), input.delaySeconds)
			needsUpdate = true
		}
		if input.maximumMessageSize != -1 && attrs["MaximumMessageSize"] != nil && input.maximumMessageSize != Atoi(*attrs["MaximumMessageSize"]) {
			Logger.Printf(PreviewString(preview)+"sqs will update attr %s for %s: %d => %d\n", "MaximumMessageSize", input.name, Atoi(*attrs["MaximumMessageSize"]), input.maximumMessageSize)
			needsUpdate = true
		}
		if input.messageRetentionPeriod != -1 && attrs["MessageRetentionPeriod"] != nil && input.messageRetentionPeriod != Atoi(*attrs["MessageRetentionPeriod"]) {
			Logger.Printf(PreviewString(preview)+"sqs will update attr %s for %s: %d => %d\n", "MessageRetentionPeriod", input.name, Atoi(*attrs["MessageRetentionPeriod"]), input.messageRetentionPeriod)
			needsUpdate = true
		}
		if input.receiveMessageWaitTimeSeconds != -1 && attrs["ReceiveMessageWaitTimeSeconds"] != nil && input.receiveMessageWaitTimeSeconds != Atoi(*attrs["ReceiveMessageWaitTimeSeconds"]) {
			Logger.Printf(PreviewString(preview)+"sqs will update attr %s for %s: %d => %d\n", "ReceiveMessageWaitTimeSeconds", input.name, Atoi(*attrs["ReceiveMessageWaitTimeSeconds"]), input.receiveMessageWaitTimeSeconds)
			needsUpdate = true
		}
		if input.visibilityTimeout != -1 && attrs["VisibilityTimeout"] != nil && input.visibilityTimeout != Atoi(*attrs["VisibilityTimeout"]) {
			Logger.Printf(PreviewString(preview)+"sqs will update attr %s for %s: %d => %d\n", "VisibilityTimeout", input.name, Atoi(*attrs["VisibilityTimeout"]), input.visibilityTimeout)
			needsUpdate = true
		}
		if input.kmsDataKeyReusePeriodSeconds != -1 && attrs["KmsDataKeyReusePeriodSeconds"] != nil && input.kmsDataKeyReusePeriodSeconds != Atoi(*attrs["KmsDataKeyReusePeriodSeconds"]) {
			Logger.Printf(PreviewString(preview)+"sqs will update attr %s for %s: %d => %d\n", "KmsDataKeyReusePeriodSeconds", input.name, Atoi(*attrs["KmsDataKeyReusePeriodSeconds"]), input.kmsDataKeyReusePeriodSeconds)
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
			Logger.Println(PreviewString(preview)+"sqs updated queue:", input.name)
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
