package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["iam-ensure-ec2-spot-roles"] = iamEnsureEC2SpotRoles
	lib.Args["iam-ensure-ec2-spot-roles"] = iamEnsureEC2SpotRolesArgs{}
}

type iamEnsureEC2SpotRolesArgs struct {
}

func (iamEnsureEC2SpotRolesArgs) Description() string {
	return "\nensure iam ec2 spot roles that are needed to use ec2 spot\n"
}

func iamEnsureEC2SpotRoles() {
	var args iamEnsureEC2SpotRolesArgs
	arg.MustParse(&args)
	ctx := context.Background()
	err := lib.IamEnsureEC2SpotRoles(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
