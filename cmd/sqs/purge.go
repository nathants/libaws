package cliaws

import (
	"context"
	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/sqs"
)

func init() {
	lib.Commands["sqs-purge"] = sqsPurge
}

type purgeArgs struct {
	QueueName string `arg:"positional,required"`
}

func (purgeArgs) Description() string {
	return "\nsqs queues purge\n"
}

func sqsPurge() {
	var args purgeArgs
	arg.MustParse(&args)
	ctx := context.Background()
	queueUrl, err := lib.SQSQueueUrl(ctx, args.QueueName)
	if err != nil {
		lib.Logger.Fatal("error:", err)
	}
	_, err = lib.SQSClient().PurgeQueue(&sqs.PurgeQueueInput{
		QueueUrl: aws.String(queueUrl),
	})
	if err != nil {
	    lib.Logger.Fatal("error:", err)
	}
}
