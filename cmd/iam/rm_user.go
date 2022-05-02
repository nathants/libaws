package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["iam-rm-user"] = iamRmUser
	lib.Args["iam-rm-user"] = iamRmUserArgs{}
}

type iamRmUserArgs struct {
	Name    string `arg:"positional,required"`
	Preview bool   `arg:"-p,--preview"`
}

func (iamRmUserArgs) Description() string {
	return "\nrm iam user\n"
}

func iamRmUser() {
	var args iamRmUserArgs
	arg.MustParse(&args)
	ctx := context.Background()
	err := lib.IamDeleteUser(ctx, args.Name, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
