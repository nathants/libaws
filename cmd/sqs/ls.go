package cliaws

import (
	"context"
	"fmt"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["sqs-ls"] = sqsLs
}

type sqsLsArgs struct {
}

func (sqsLsArgs) Description() string {
	return "\nlist sqs queues\n"
}

func sqsLs() {
	var args sqsLsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	queues, err := lib.SQSListQueues(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, queue := range queues {
		parts := strings.Split(*queue, "/")
		fmt.Println(parts[len(parts)-1])
	}
}
