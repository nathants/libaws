package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["aws-account"] = account
}

type lsArgs struct {
}

func (lsArgs) Description() string {
	return "\ncurrent account id\n"
}

func account() {
	var args lsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	account, err := lib.Account(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Println(account)
}
