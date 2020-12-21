package cliaws

import (
	"strings"
	"context"
	"fmt"
	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["sqs-ls"] = sqsLs
}

type lsArgs struct {
}

func (lsArgs) Description() string {
	return "\nlist sqs queues\n"
}

func sqsLs() {
	var args lsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	queues, err := lib.SqsListQueues(ctx)
	if err != nil {
	    lib.Logger.Fatal("error:", err)
	}
	for _, queue := range queues {
		parts := strings.Split(*queue, "/")
		fmt.Println(parts[len(parts)-1])
	}
}
