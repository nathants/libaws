package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
	"github.com/sethvargo/go-password/password"
)

func init() {
	lib.Commands["iam-ensure-user"] = iamEnsureUser
}

type iamEnsureUserArgs struct {
	Username   string `arg:"positional,required"`
	PolicyName string `arg:"--policy-name,required"`
	Preview    bool   `arg:"-p,--preview"`
}

func (iamEnsureUserArgs) Description() string {
	return "\nensure an iam user\n"
}

func iamEnsureUser() {
	var args iamEnsureUserArgs
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
	err = lib.IamEnsureUser(ctx, args.Username, password, args.PolicyName, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println("username:", args.Username)
	fmt.Println("password:", password)
	fmt.Printf("url: https://%s.signin.aws.amazon.com/console\n", account)
}
