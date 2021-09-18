package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["sqs-rm"] = sqsRm
	lib.Args["sqs-rm"] = sqsRmArgs{}
}

type sqsRmArgs struct {
	QueueName string `arg:"positional,required"`
	Yes       bool   `arg:"-y,--yes" default:"false"`
}

func (sqsRmArgs) Description() string {
	return "\ndelete an sqs queue\n"
}

func sqsRm() {
	var args sqsRmArgs
	arg.MustParse(&args)
	ctx := context.Background()
	queueUrl, err := lib.SQSQueueUrl(ctx, args.QueueName)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	lib.Logger.Println("going to delete:", queueUrl)
	if !args.Yes {
		err = lib.PromptProceed("")
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
	}
	_, err = lib.SQSClient().DeleteQueueWithContext(ctx, &sqs.DeleteQueueInput{
		QueueUrl: aws.String(queueUrl),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
