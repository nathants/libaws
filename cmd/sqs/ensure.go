package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["sqs-ensure"] = sqsEnsure
lib.Args["sqs-ensure"] = sqsEnsureArgs{}
}

type sqsEnsureArgs struct {
	Name    string   `arg:"positional,required"`
	Attrs   []string `arg:"positional,required"`
	Preview bool     `arg:"-p,--preview"`
}

func (sqsEnsureArgs) Description() string {
	return `
ensure a sqs queue

example:
 - cli-aws sqs-ensure test-queue DelaySeconds=30 VisibilityTimeout=60

optional attrs:
 - DelaySeconds=VALUE
 - MaximumMessageSize=VALUE
 - MessageRetentionPeriod=VALUE
 - ReceiveMessageWaitTimeSeconds=VALUE
 - VisibilityTimeout=VALUE
 - KmsDataKeyReusePeriodSeconds=VALUE

`
}

func sqsEnsure() {
	var args sqsEnsureArgs
	arg.MustParse(&args)
	ctx := context.Background()
	input, err := lib.SQSEnsureInput(args.Name, args.Attrs)
	if err != nil {
	    lib.Logger.Fatal("error: ", err)
	}
	err = lib.SQSEnsure(ctx, input, args.Preview)
	if err != nil {
	    lib.Logger.Fatal("error: ", err)
	}
}
