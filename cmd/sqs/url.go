package libaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["sqs-url"] = sqsUrl
	lib.Args["sqs-url"] = sqsUrlArgs{}
}

type sqsUrlArgs struct {
	QueueName string `arg:"positional,required"`
	Preview   bool   `arg:"-p,--preview"`
}

func (sqsUrlArgs) Description() string {
	return "\ndelete an sqs queue\n"
}

func sqsUrl() {
	var args sqsUrlArgs
	arg.MustParse(&args)
	ctx := context.Background()
	queueUrl, err := lib.SQSQueueUrl(ctx, args.QueueName)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(queueUrl)
}
