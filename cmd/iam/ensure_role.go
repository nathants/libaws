package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["iam-ensure-role"] = iamEnsureRole
	lib.Args["iam-ensure-role"] = iamEnsureRoleArgs{}
}

type iamEnsureRoleArgs struct {
	Name      string   `arg:"positional,required"`
	Principal string   `arg:"positional,required"`
	Policies  []string `arg:"--policy"`
	Allows    []string `arg:"--allow"`
	Preview   bool     `arg:"-p,--preview"`
}

func (iamEnsureRoleArgs) Description() string {
	return "\nensure an iam role\n"
}

func iamEnsureRole() {
	var args iamEnsureRoleArgs
	arg.MustParse(&args)
	ctx := context.Background()
	err := lib.IamEnsureRole(ctx, args.Name, args.Principal, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	err = lib.IamEnsureRolePolicies(ctx, args.Name, args.Policies, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	err = lib.IamEnsureRoleAllows(ctx, args.Name, args.Allows, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
