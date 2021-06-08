package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["sqs-stats"] = sqsStats
}

type sqsStatsArgs struct {
	QueueName string `arg:"positional,required"`
}

func (sqsStatsArgs) Description() string {
	return "\nsqs queues stats\n"
}

func sqsStats() {
	var args sqsStatsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	queueUrl, err := lib.SQSQueueUrl(ctx, args.QueueName)
	if err != nil {
		lib.Logger.Fatal("error:", err)
	}
	out, err := lib.SQSNumMessages(ctx, queueUrl)
	if err != nil {
		lib.Logger.Fatal("error:", err)
	}
	fmt.Println(lib.Pformat(out))
}
