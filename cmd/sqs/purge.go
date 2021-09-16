package cliaws

import (
	"context"
	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["sqs-purge"] = sqsPurge
lib.Args["sqs-purge"] = sqsPurgeArgs{}
}

type sqsPurgeArgs struct {
	QueueName string `arg:"positional,required"`
}

func (sqsPurgeArgs) Description() string {
	return "\nsqs queues purge\n"
}

func sqsPurge() {
	var args sqsPurgeArgs
	arg.MustParse(&args)
	ctx := context.Background()
	queueUrl, err := lib.SQSQueueUrl(ctx, args.QueueName)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	_, err = lib.SQSClient().PurgeQueue(&sqs.PurgeQueueInput{
		QueueUrl: aws.String(queueUrl),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
