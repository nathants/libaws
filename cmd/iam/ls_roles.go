package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["iam-ls-roles"] = iamLsRoles
	lib.Args["iam-ls-roles"] = iamLsRolesArgs{}
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
	roles, err := lib.IamListRoles(ctx, nil)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, role := range roles {
		fmt.Println(lib.Pformat(role))
	}
}
