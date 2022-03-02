package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["iam-ensure-ec2-roles"] = iamEnsureEC2Roles
	lib.Args["iam-ensure-ec2-roles"] = iamEnsureEC2RolesArgs{}
}

type iamEnsureEC2RolesArgs struct {
}

func (iamEnsureEC2RolesArgs) Description() string {
	return "\nensure iam ec2 spot roles that are needed to use ec2 spot\n"
}

func iamEnsureEC2Roles() {
	var args iamEnsureEC2RolesArgs
	arg.MustParse(&args)
	ctx := context.Background()
	err := lib.IamEnsureEC2Roles(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
