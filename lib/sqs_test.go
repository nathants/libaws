package lib

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/gofrs/uuid"
)

func checkAccountSQS() {
	account, err := StsAccount(context.Background())
	if err != nil {
		panic(err)
	}
	if os.Getenv("LIBAWS_TEST_ACCOUNT") != account {
		panic(fmt.Sprintf("%s != %s", os.Getenv("LIBAWS_TEST_ACCOUNT"), account))
	}
}

func TestSQSEnsure(t *testing.T) {
	checkAccountSQS()
	queue := "libaws-sqs-test-" + uuid.Must(uuid.NewV4()).String()
	input, err := SQSEnsureInput("", queue, []string{})
	if err != nil {
		t.Error(err)
		return
	}
	ctx := context.Background()
	err = SQSEnsure(ctx, input, false)
	if err != nil {
		t.Error(err)
		return
	}
	defer func() {
		err := SQSDeleteQueue(ctx, queue, false)
		if err != nil {
			panic(err)
		}
	}()
}

func TestSQSEnsureDelaySeconds(t *testing.T) {
	checkAccountSQS()
	queue := "libaws-sqs-test-" + uuid.Must(uuid.NewV4()).String()
	ctx := context.Background()
	input, err := SQSEnsureInput("", queue, []string{"DelaySeconds=7"})
	if err != nil {
		t.Error(err)
		return
	}
	err = SQSEnsure(ctx, input, false)
	if err != nil {
		t.Error(err)
		return
	}
	defer func() {
		err := SQSDeleteQueue(ctx, queue, false)
		if err != nil {
			panic(err)
		}
	}()
	url, err := SQSQueueUrl(ctx, queue)
	if err != nil {
		t.Error(err)
		return
	}
	attrs, err := SQSClient().GetQueueAttributesWithContext(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(url),
		AttributeNames: []*string{aws.String("DelaySeconds"), aws.String("MaximumMessageSize"), aws.String("MessageRetentionPeriod"), aws.String("ReceiveMessageWaitTimeSeconds"), aws.String("VisibilityTimeout")},
	})
	if err != nil {
		t.Error(err)
		return
	}
	if *attrs.Attributes["DelaySeconds"] != "7" {
		t.Errorf("expected 7, got %s", *attrs.Attributes["DelaySeconds"])
		return
	}
	//
	input, err = SQSEnsureInput("", queue, []string{"DelaySeconds=15"})
	if err != nil {
		t.Error(err)
		return
	}
	err = SQSEnsure(ctx, input, false)
	if err != nil {
		t.Error(err)
		return
	}
	attrs, err = SQSClient().GetQueueAttributesWithContext(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(url),
		AttributeNames: []*string{aws.String("DelaySeconds"), aws.String("MaximumMessageSize"), aws.String("MessageRetentionPeriod"), aws.String("ReceiveMessageWaitTimeSeconds"), aws.String("VisibilityTimeout")},
	})
	if err != nil {
		t.Error(err)
		return
	}
	if *attrs.Attributes["DelaySeconds"] != "15" {
		t.Errorf("expected 15, got %s", *attrs.Attributes["DelaySeconds"])
		return
	}
}

func TestSQSEnsureMaximumMessageSize(t *testing.T) {
	checkAccountSQS()
	queue := "libaws-sqs-test-" + uuid.Must(uuid.NewV4()).String()
	ctx := context.Background()
	input, err := SQSEnsureInput("", queue, []string{"MaximumMessageSize=2048"})
	if err != nil {
		t.Error(err)
		return
	}
	err = SQSEnsure(ctx, input, false)
	if err != nil {
		t.Error(err)
		return
	}
	defer func() {
		err := SQSDeleteQueue(ctx, queue, false)
		if err != nil {
			panic(err)
		}
	}()
	url, err := SQSQueueUrl(ctx, queue)
	if err != nil {
		t.Error(err)
		return
	}
	attrs, err := SQSClient().GetQueueAttributesWithContext(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(url),
		AttributeNames: []*string{aws.String("MaximumMessageSize"), aws.String("MaximumMessageSize"), aws.String("MessageRetentionPeriod"), aws.String("ReceiveMessageWaitTimeSeconds"), aws.String("VisibilityTimeout")},
	})
	if err != nil {
		t.Error(err)
		return
	}
	if *attrs.Attributes["MaximumMessageSize"] != "2048" {
		t.Errorf("expected 2048, got %s", *attrs.Attributes["MaximumMessageSize"])
		return
	}
	//
	input, err = SQSEnsureInput("", queue, []string{"MaximumMessageSize=4096"})
	if err != nil {
		t.Error(err)
		return
	}
	err = SQSEnsure(ctx, input, false)
	if err != nil {
		t.Error(err)
		return
	}
	attrs, err = SQSClient().GetQueueAttributesWithContext(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(url),
		AttributeNames: []*string{aws.String("MaximumMessageSize"), aws.String("MaximumMessageSize"), aws.String("MessageRetentionPeriod"), aws.String("ReceiveMessageWaitTimeSeconds"), aws.String("VisibilityTimeout")},
	})
	if err != nil {
		t.Error(err)
		return
	}
	if *attrs.Attributes["MaximumMessageSize"] != "4096" {
		t.Errorf("expected 4096, got %s", *attrs.Attributes["MaximumMessageSize"])
		return
	}
}

func TestSQSEnsureMessageRetentionPeriod(t *testing.T) {
	checkAccountSQS()
	queue := "libaws-sqs-test-" + uuid.Must(uuid.NewV4()).String()
	ctx := context.Background()
	input, err := SQSEnsureInput("", queue, []string{"MessageRetentionPeriod=90"})
	if err != nil {
		t.Error(err)
		return
	}
	err = SQSEnsure(ctx, input, false)
	if err != nil {
		t.Error(err)
		return
	}
	defer func() {
		err := SQSDeleteQueue(ctx, queue, false)
		if err != nil {
			panic(err)
		}
	}()
	url, err := SQSQueueUrl(ctx, queue)
	if err != nil {
		t.Error(err)
		return
	}
	attrs, err := SQSClient().GetQueueAttributesWithContext(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(url),
		AttributeNames: []*string{aws.String("MessageRetentionPeriod"), aws.String("MaximumMessageSize"), aws.String("MessageRetentionPeriod"), aws.String("ReceiveMessageWaitTimeSeconds"), aws.String("VisibilityTimeout")},
	})
	if err != nil {
		t.Error(err)
		return
	}
	if *attrs.Attributes["MessageRetentionPeriod"] != "90" {
		t.Errorf("expected 90, got %s", *attrs.Attributes["MessageRetentionPeriod"])
		return
	}
	//
	input, err = SQSEnsureInput("", queue, []string{"MessageRetentionPeriod=120"})
	if err != nil {
		t.Error(err)
		return
	}
	err = SQSEnsure(ctx, input, false)
	if err != nil {
		t.Error(err)
		return
	}
	attrs, err = SQSClient().GetQueueAttributesWithContext(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(url),
		AttributeNames: []*string{aws.String("MessageRetentionPeriod"), aws.String("MaximumMessageSize"), aws.String("MessageRetentionPeriod"), aws.String("ReceiveMessageWaitTimeSeconds"), aws.String("VisibilityTimeout")},
	})
	if err != nil {
		t.Error(err)
		return
	}
	if *attrs.Attributes["MessageRetentionPeriod"] != "120" {
		t.Errorf("expected 120, got %s", *attrs.Attributes["MessageRetentionPeriod"])
		return
	}
}

func TestSQSEnsureReceiveMessageWaitTimeSeconds(t *testing.T) {
	checkAccountSQS()
	queue := "libaws-sqs-test-" + uuid.Must(uuid.NewV4()).String()
	ctx := context.Background()
	input, err := SQSEnsureInput("", queue, []string{"ReceiveMessageWaitTimeSeconds=7"})
	if err != nil {
		t.Error(err)
		return
	}
	err = SQSEnsure(ctx, input, false)
	if err != nil {
		t.Error(err)
		return
	}
	defer func() {
		err := SQSDeleteQueue(ctx, queue, false)
		if err != nil {
			panic(err)
		}
	}()
	url, err := SQSQueueUrl(ctx, queue)
	if err != nil {
		t.Error(err)
		return
	}
	attrs, err := SQSClient().GetQueueAttributesWithContext(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(url),
		AttributeNames: []*string{aws.String("ReceiveMessageWaitTimeSeconds"), aws.String("MaximumMessageSize"), aws.String("MessageRetentionPeriod"), aws.String("ReceiveMessageWaitTimeSeconds"), aws.String("VisibilityTimeout")},
	})
	if err != nil {
		t.Error(err)
		return
	}
	if *attrs.Attributes["ReceiveMessageWaitTimeSeconds"] != "7" {
		t.Errorf("expected 7, got %s", *attrs.Attributes["ReceiveMessageWaitTimeSeconds"])
		return
	}
	//
	input, err = SQSEnsureInput("", queue, []string{"ReceiveMessageWaitTimeSeconds=3"})
	if err != nil {
		t.Error(err)
		return
	}
	err = SQSEnsure(ctx, input, false)
	if err != nil {
		t.Error(err)
		return
	}
	attrs, err = SQSClient().GetQueueAttributesWithContext(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(url),
		AttributeNames: []*string{aws.String("ReceiveMessageWaitTimeSeconds"), aws.String("MaximumMessageSize"), aws.String("MessageRetentionPeriod"), aws.String("ReceiveMessageWaitTimeSeconds"), aws.String("VisibilityTimeout")},
	})
	if err != nil {
		t.Error(err)
		return
	}
	if *attrs.Attributes["ReceiveMessageWaitTimeSeconds"] != "3" {
		t.Errorf("expected 3, got %s", *attrs.Attributes["ReceiveMessageWaitTimeSeconds"])
		return
	}
}

func TestSQSEnsureVisibilityTimeout(t *testing.T) {
	checkAccountSQS()
	queue := "libaws-sqs-test-" + uuid.Must(uuid.NewV4()).String()
	ctx := context.Background()
	input, err := SQSEnsureInput("", queue, []string{"VisibilityTimeout=7"})
	if err != nil {
		t.Error(err)
		return
	}
	err = SQSEnsure(ctx, input, false)
	if err != nil {
		t.Error(err)
		return
	}
	defer func() {
		err := SQSDeleteQueue(ctx, queue, false)
		if err != nil {
			panic(err)
		}
	}()
	url, err := SQSQueueUrl(ctx, queue)
	if err != nil {
		t.Error(err)
		return
	}
	attrs, err := SQSClient().GetQueueAttributesWithContext(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(url),
		AttributeNames: []*string{aws.String("VisibilityTimeout"), aws.String("MaximumMessageSize"), aws.String("MessageRetentionPeriod"), aws.String("ReceiveMessageWaitTimeSeconds"), aws.String("VisibilityTimeout")},
	})
	if err != nil {
		t.Error(err)
		return
	}
	if *attrs.Attributes["VisibilityTimeout"] != "7" {
		t.Errorf("expected 7, got %s", *attrs.Attributes["VisibilityTimeout"])
		return
	}
	//
	input, err = SQSEnsureInput("", queue, []string{"VisibilityTimeout=15"})
	if err != nil {
		t.Error(err)
		return
	}
	err = SQSEnsure(ctx, input, false)
	if err != nil {
		t.Error(err)
		return
	}
	attrs, err = SQSClient().GetQueueAttributesWithContext(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(url),
		AttributeNames: []*string{aws.String("VisibilityTimeout"), aws.String("MaximumMessageSize"), aws.String("MessageRetentionPeriod"), aws.String("ReceiveMessageWaitTimeSeconds"), aws.String("VisibilityTimeout")},
	})
	if err != nil {
		t.Error(err)
		return
	}
	if *attrs.Attributes["VisibilityTimeout"] != "15" {
		t.Errorf("expected 15, got %s", *attrs.Attributes["VisibilityTimeout"])
		return
	}
}
