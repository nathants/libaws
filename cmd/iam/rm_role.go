package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["iam-rm-role"] = iamRmRole
	lib.Args["iam-rm-role"] = iamRmRoleArgs{}
}

type iamRmRoleArgs struct {
	Name    string `arg:"positional,required"`
	Preview bool   `arg:"-p,--preview"`
}

func (iamRmRoleArgs) Description() string {
	return "\nrm iam role\n"
}

func iamRmRole() {
	var args iamRmRoleArgs
	arg.MustParse(&args)
	ctx := context.Background()
	err := lib.IamDeleteRole(ctx, args.Name, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
