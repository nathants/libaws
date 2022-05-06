package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
	"github.com/sethvargo/go-password/password"
)

func init() {
	lib.Commands["iam-reset-user-login-password"] = iamResetUserLoginPassword
	lib.Args["iam-reset-user-login-password"] = iamResetUserLoginPasswordArgs{}
}

type iamResetUserLoginPasswordArgs struct {
	User string `arg:"positional,required"`
}

func (iamResetUserLoginPasswordArgs) Description() string {
	return "\nreset an iam user login password\n"
}

func iamResetUserLoginPassword() {
	var args iamResetUserLoginPasswordArgs
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
	err = lib.IamResetUserLoginTempPassword(ctx, args.User, password)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println("password:", password)
	fmt.Printf("url: https://%s.signin.aws.amazon.com/console\n", account)
}
