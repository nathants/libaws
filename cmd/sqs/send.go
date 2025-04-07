package libaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["sqs-send"] = sqsSend
	lib.Args["sqs-send"] = sqsSendArgs{}
}

type sqsSendArgs struct {
	Name    string `arg:"positional,required"`
	Message string `arg:"positional,required"`
}

func (sqsSendArgs) Description() string {
	return "\nsend a message to a sqs queue\n"
}

func sqsSend() {
	var args sqsSendArgs
	arg.MustParse(&args)
	ctx := context.Background()
	url, err := lib.SQSQueueUrl(ctx, args.Name)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	_, err = lib.SQSClient().SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(url),
		MessageBody: aws.String(args.Message),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
