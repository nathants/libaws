package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["iam-ensure-user-api"] = iamEnsureUserApi
	lib.Args["iam-ensure-user-api"] = iamEnsureUserApiArgs{}
}

type iamEnsureUserApiArgs struct {
	Name     string   `arg:"positional,required"`
	Policies []string `arg:"--policy"`
	Allows   []string `arg:"--allow"`
	Preview  bool     `arg:"-p,--preview"`
}

func (iamEnsureUserApiArgs) Description() string {
	return "\nensure an iam user with api key\n"
}

func iamEnsureUserApi() {
	var args iamEnsureUserApiArgs
	arg.MustParse(&args)
	ctx := context.Background()
	out, err := lib.IamEnsureUserApi(ctx, args.Name, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	err = lib.IamEnsureUserPolicies(ctx, args.Name, args.Policies, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	err = lib.IamEnsureUserAllows(ctx, args.Name, args.Allows, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	if out.AccessKeyId != nil {
		fmt.Println("access key id:", *out.AccessKeyId)
		fmt.Println("access key secret:", *out.SecretAccessKey)
	}
}
