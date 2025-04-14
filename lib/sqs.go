package lib

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

var sqsClient *sqs.Client
var sqsClientLock sync.Mutex

func SQSClientExplicit(accessKeyID, accessKeySecret, region string) *sqs.Client {
	return sqs.NewFromConfig(*SessionExplicit(accessKeyID, accessKeySecret, region))
}

func SQSClient() *sqs.Client {
	sqsClientLock.Lock()
	defer sqsClientLock.Unlock()
	if sqsClient == nil {
		sqsClient = sqs.NewFromConfig(*Session())
	}
	return sqsClient
}

func SQSListQueues(ctx context.Context) ([]string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "SQSListQueues"}
		defer d.Log()
	}
	var nextToken *string
	var queues []string
	for {
		out, err := SQSClient().ListQueues(ctx, &sqs.ListQueuesInput{
			NextToken: nextToken,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		for _, queue := range out.QueueUrls {
			queues = append(queues, Last(strings.Split(queue, "/")))
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
	if doDebug {
		d := &Debug{start: time.Now(), name: "SQSListQueueUrls"}
		defer d.Log()
	}
	var nextToken *string
	var queues []string
	for {
		out, err := SQSClient().ListQueues(ctx, &sqs.ListQueuesInput{
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

func SQSQueueUrl(ctx context.Context, name string) (string, error) {
	account, err := StsAccount(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	return fmt.Sprintf("https://sqs.%s.amazonaws.com/%s/%s", Region(), account, name), nil
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
	if doDebug {
		d := &Debug{start: time.Now(), name: "SQSNumMessages"}
		defer d.Log()
	}
	out, err := SQSClient().GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl: aws.String(queueUrl),
		AttributeNames: []sqstypes.QueueAttributeName{
			sqstypes.QueueAttributeNameApproximateNumberOfMessages,
			sqstypes.QueueAttributeNameApproximateNumberOfMessagesNotVisible,
			sqstypes.QueueAttributeNameApproximateNumberOfMessagesDelayed,
		},
	})
	if err != nil {
		return nil, err
	}
	return &SQSNumMessageOutput{
		Atoi(out.Attributes[string(sqstypes.QueueAttributeNameApproximateNumberOfMessages)]),
		Atoi(out.Attributes[string(sqstypes.QueueAttributeNameApproximateNumberOfMessagesNotVisible)]),
		Atoi(out.Attributes[string(sqstypes.QueueAttributeNameApproximateNumberOfMessagesDelayed)]),
	}, nil
}

type sqsEnsureInput struct {
	infraSetName                  string
	name                          string
	delaySeconds                  int
	maximumMessageSize            int
	messageRetentionPeriod        int
	receiveMessageWaitTimeSeconds int
	visibilityTimeout             int
	kmsDataKeyReusePeriodSeconds  int
}

func (input *sqsEnsureInput) Attrs() map[string]string {
	m := map[string]string{}
	m["KmsMasterKeyId"] = "alias/aws/sqs"
	if input.delaySeconds != -1 {
		m["DelaySeconds"] = fmt.Sprint(input.delaySeconds)
	}
	if input.maximumMessageSize != -1 {
		m["MaximumMessageSize"] = fmt.Sprint(input.maximumMessageSize)
	}
	if input.messageRetentionPeriod != -1 {
		m["MessageRetentionPeriod"] = fmt.Sprint(input.messageRetentionPeriod)
	}
	if input.receiveMessageWaitTimeSeconds != -1 {
		m["ReceiveMessageWaitTimeSeconds"] = fmt.Sprint(input.receiveMessageWaitTimeSeconds)
	}
	if input.visibilityTimeout != -1 {
		m["VisibilityTimeout"] = fmt.Sprint(input.visibilityTimeout)
	}
	if input.kmsDataKeyReusePeriodSeconds != -1 {
		m["KmsDataKeyReusePeriodSeconds"] = fmt.Sprint(input.kmsDataKeyReusePeriodSeconds)
	}
	if len(m) != 0 {
		return m
	}
	return nil
}

func SQSEnsureInput(infraSetName, queueName string, attrs []string) (*sqsEnsureInput, error) {
	input := &sqsEnsureInput{
		infraSetName:                  infraSetName,
		name:                          queueName,
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
		case "delayseconds", "delay":
			num, err := strconv.Atoi(value)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			input.delaySeconds = num
		case "maximummessagesize", "size":
			num, err := strconv.Atoi(value)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			input.maximumMessageSize = num
		case "messageretentionperiod", "retention":
			num, err := strconv.Atoi(value)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			input.messageRetentionPeriod = num
		case "receivemessagewaittimeseconds", "wait":
			num, err := strconv.Atoi(value)
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			input.receiveMessageWaitTimeSeconds = num
		case "visibilitytimeout", "timeout":
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
	if doDebug {
		d := &Debug{start: time.Now(), name: "SQSEnsure"}
		defer d.Log()
	}
	sqsUrl, err := SQSQueueUrl(ctx, input.name)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	attrsOut, err := SQSClient().GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl: aws.String(sqsUrl),
		AttributeNames: []sqstypes.QueueAttributeName{
			sqstypes.QueueAttributeNameDelaySeconds,
			sqstypes.QueueAttributeNameMaximumMessageSize,
			sqstypes.QueueAttributeNameMessageRetentionPeriod,
			sqstypes.QueueAttributeNameReceiveMessageWaitTimeSeconds,
			sqstypes.QueueAttributeNameVisibilityTimeout,
			sqstypes.QueueAttributeNameKmsDataKeyReusePeriodSeconds,
		},
	})
	if err != nil {
		if !strings.Contains(err.Error(), "AWS.SimpleQueueService.NonExistentQueue") {
			Logger.Println("error:", err)
			return err
		}
		if !preview {
			_, err := SQSClient().CreateQueue(ctx, &sqs.CreateQueueInput{
				QueueName:  aws.String(input.name),
				Attributes: input.Attrs(),
				Tags: map[string]string{
					infraSetTagName: input.infraSetName,
				},
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			for {
				queues, err := SQSListQueues(ctx)
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				if slices.Contains(queues, input.name) {
					break
				}
				time.Sleep(1 * time.Second)
				Logger.Println("waiting for sqs instantiation for:", input.name)
			}
		}
		Logger.Printf(PreviewString(preview)+"created queue: %s\n", input.name)
		for k, v := range input.Attrs() {
			if v != "" && k != "KmsMasterKeyId" {
				Logger.Println(PreviewString(preview)+"created attribute for", input.name+":", k, "=", v)
			}
		}
	} else {
		needsUpdate := false
		attrs := attrsOut.Attributes
		if input.delaySeconds != -1 && attrs["DelaySeconds"] != "" && input.delaySeconds != Atoi(attrs["DelaySeconds"]) {
			Logger.Printf(PreviewString(preview)+"will update attr %s for %s: %d => %d\n", "DelaySeconds", input.name, Atoi(attrs["DelaySeconds"]), input.delaySeconds)
			needsUpdate = true
		}
		if input.maximumMessageSize != -1 && attrs["MaximumMessageSize"] != "" && input.maximumMessageSize != Atoi(attrs["MaximumMessageSize"]) {
			Logger.Printf(PreviewString(preview)+"will update attr %s for %s: %d => %d\n", "MaximumMessageSize", input.name, Atoi(attrs["MaximumMessageSize"]), input.maximumMessageSize)
			needsUpdate = true
		}
		if input.messageRetentionPeriod != -1 && attrs["MessageRetentionPeriod"] != "" && input.messageRetentionPeriod != Atoi(attrs["MessageRetentionPeriod"]) {
			Logger.Printf(PreviewString(preview)+"will update attr %s for %s: %d => %d\n", "MessageRetentionPeriod", input.name, Atoi(attrs["MessageRetentionPeriod"]), input.messageRetentionPeriod)
			needsUpdate = true
		}
		if input.receiveMessageWaitTimeSeconds != -1 && attrs["ReceiveMessageWaitTimeSeconds"] != "" && input.receiveMessageWaitTimeSeconds != Atoi(attrs["ReceiveMessageWaitTimeSeconds"]) {
			Logger.Printf(PreviewString(preview)+"will update attr %s for %s: %d => %d\n", "ReceiveMessageWaitTimeSeconds", input.name, Atoi(attrs["ReceiveMessageWaitTimeSeconds"]), input.receiveMessageWaitTimeSeconds)
			needsUpdate = true
		}
		if input.visibilityTimeout != -1 && attrs["VisibilityTimeout"] != "" && input.visibilityTimeout != Atoi(attrs["VisibilityTimeout"]) {
			Logger.Printf(PreviewString(preview)+"will update attr %s for %s: %d => %d\n", "VisibilityTimeout", input.name, Atoi(attrs["VisibilityTimeout"]), input.visibilityTimeout)
			needsUpdate = true
		}
		if input.kmsDataKeyReusePeriodSeconds != -1 && attrs["KmsDataKeyReusePeriodSeconds"] != "" && input.kmsDataKeyReusePeriodSeconds != Atoi(attrs["KmsDataKeyReusePeriodSeconds"]) {
			Logger.Printf(PreviewString(preview)+"will update attr %s for %s: %d => %d\n", "KmsDataKeyReusePeriodSeconds", input.name, Atoi(attrs["KmsDataKeyReusePeriodSeconds"]), input.kmsDataKeyReusePeriodSeconds)
			needsUpdate = true
		}
		if needsUpdate {
			if !preview {
				_, err := SQSClient().SetQueueAttributes(ctx, &sqs.SetQueueAttributesInput{
					QueueUrl:   aws.String(sqsUrl),
					Attributes: input.Attrs(),
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Println(PreviewString(preview)+"updated queue:", input.name)
		}
	}
	return nil
}

func SQSDeleteQueue(ctx context.Context, name string, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "SQSDeleteQueue"}
		defer d.Log()
	}
	url, err := SQSQueueUrl(ctx, name)
	if err != nil {
		Logger.Println("error:", err)
		return nil
	}
	if !preview {
		_, err := SQSClient().DeleteQueue(ctx, &sqs.DeleteQueueInput{
			QueueUrl: aws.String(url),
		})
		if err != nil {
			if !strings.Contains(err.Error(), "AWS.SimpleQueueService.NonExistentQueue") {
				return err
			}
		}
	}
	Logger.Println(PreviewString(preview)+"deleted queue:", name)
	return nil
}
