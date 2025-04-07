package libaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["iam-ensure-user-api"] = iamEnsureUserApi
	lib.Args["iam-ensure-user-api"] = iamEnsureUserApiArgs{}
}

type iamEnsureUserApiArgs struct {
	Name    string   `arg:"positional,required"`
	Policy  []string `arg:"--policy" help:"policy name, can specify multiple values"`
	Allow   []string `arg:"--allow" help:"\"$service:$action $arn\", example: \"s3:* *\", can specify multiple values"`
	Preview bool     `arg:"-p,--preview"`
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
	err = lib.IamEnsureUserPolicies(ctx, args.Name, args.Policy, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	err = lib.IamEnsureUserAllows(ctx, args.Name, args.Allow, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	if out.AccessKeyId != nil {
		fmt.Println("access key id:", *out.AccessKeyId)
		fmt.Println("access key secret:", *out.SecretAccessKey)
	}
}
