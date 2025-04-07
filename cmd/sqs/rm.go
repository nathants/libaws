package libaws

import (
	"context"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["sqs-rm"] = sqsRm
	lib.Args["sqs-rm"] = sqsRmArgs{}
}

type sqsRmArgs struct {
	QueueName string `arg:"positional,required"`
	Preview   bool   `arg:"-p,--preview"`
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
	lib.Logger.Println(lib.PreviewString(args.Preview)+"deleted:", queueUrl)
	if args.Preview {
		os.Exit(0)
	}
	_, err = lib.SQSClient().DeleteQueue(ctx, &sqs.DeleteQueueInput{
		QueueUrl: aws.String(queueUrl),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
