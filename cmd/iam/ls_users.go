package libaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["iam-ls-users"] = iamLsUsers
	lib.Args["iam-ls-users"] = iamLsUsersArgs{}
}

type iamLsUsersArgs struct {
}

func (iamLsUsersArgs) Description() string {
	return "\nlist iam users\n"
}

func iamLsUsers() {
	var args iamLsUsersArgs
	arg.MustParse(&args)
	ctx := context.Background()
	users, err := lib.IamListUsers(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, role := range users {
		fmt.Println(lib.Pformat(role))
	}
}
