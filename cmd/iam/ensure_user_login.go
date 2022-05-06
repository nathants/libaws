package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
	"github.com/sethvargo/go-password/password"
)

func init() {
	lib.Commands["iam-ensure-user-login"] = iamEnsureUserLogin
	lib.Args["iam-ensure-user-login"] = iamEnsureUserLoginArgs{}
}

type iamEnsureUserLoginArgs struct {
	Name     string   `arg:"positional,required"`
	Policies []string `arg:"--policy"`
	Allows   []string `arg:"--allow"`
	Preview  bool     `arg:"-p,--preview"`
}

func (iamEnsureUserLoginArgs) Description() string {
	return "\nensure an iam user with login\n"
}

func iamEnsureUserLogin() {
	var args iamEnsureUserLoginArgs
	arg.MustParse(&args)
	ctx := context.Background()
	account, err := lib.StsAccount(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	password, err := password.Generate(24, 4, 4, false, false)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	err = lib.IamEnsureUserLogin(ctx, args.Name, password, args.Preview)
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
	fmt.Println("username:", args.Name)
	fmt.Println("password:", password)
	fmt.Printf("url: https://%s.signin.aws.amazon.com/console\n", account)
}
