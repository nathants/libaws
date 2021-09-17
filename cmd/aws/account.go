package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["aws-account"] = account
	lib.Args["aws-account"] = accountArgs{}
}

type accountArgs struct {
}

func (accountArgs) Description() string {
	return "\ncurrent account id\n"
}

func account() {
	var args accountArgs
	arg.MustParse(&args)
	ctx := context.Background()
	account, err := lib.StsAccount(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Println(account)
}
