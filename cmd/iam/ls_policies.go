package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["iam-ls-policies"] = iamLsPolicies
lib.Args["iam-ls-policies"] = iamLsPoliciesArgs{}
}

type iamLsPoliciesArgs struct {
}

func (iamLsPoliciesArgs) Description() string {
	return "\nlist iam policies\n"
}

func iamLsPolicies() {
	var args iamLsPoliciesArgs
	arg.MustParse(&args)
	ctx := context.Background()
	policies, err := lib.IamListPolicies(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, policy := range policies {
		fmt.Println(lib.Pformat(policy))
	}
}
