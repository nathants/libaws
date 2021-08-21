package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["iam-ls-roles"] = iamLsRoles
}

type iamLsRolesArgs struct {
}

func (iamLsRolesArgs) Description() string {
	return "\nlist iam roles\n"
}

func iamLsRoles() {
	var args iamLsRolesArgs
	arg.MustParse(&args)
	ctx := context.Background()
	roles, err := lib.IamListRoles(ctx, "")
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, role := range roles {
		fmt.Println(lib.Pformat(role))
	}
}
