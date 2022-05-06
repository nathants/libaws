package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["sqs-ensure"] = sqsEnsure
	lib.Args["sqs-ensure"] = sqsEnsureArgs{}
}

type sqsEnsureArgs struct {
	Name    string   `arg:"positional,required"`
	Attr    []string `arg:"positional"`
	Preview bool     `arg:"-p,--preview"`
}

func (sqsEnsureArgs) Description() string {
	return `
ensure a sqs queue

example:
 - libaws sqs-ensure test-queue delay=30 timeout=60

optional attrs:
 - DelaySeconds=VALUE,                  shortcut: delay=VALUE,     default: 0
 - MaximumMessageSize=VALUE,            shortcut: size=VALUE,      default: 262144
 - MessageRetentionPeriod=VALUE,        shortcut: retention=VALUE  default: 345600
 - ReceiveMessageWaitTimeSeconds=VALUE, shortcut: wait=VALUE       default: 0
 - VisibilityTimeout=VALUE,             shortcut: timeout=VALUE    default: 30

`
}

func sqsEnsure() {
	var args sqsEnsureArgs
	arg.MustParse(&args)
	ctx := context.Background()
	input, err := lib.SQSEnsureInput("", args.Name, args.Attr)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	err = lib.SQSEnsure(ctx, input, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
