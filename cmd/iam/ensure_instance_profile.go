package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["iam-ensure-instance-profile"] = iamEnsureInstanceProfile
	lib.Args["iam-ensure-instance-profile"] = iamEnsureInstanceProfileArgs{}
}

type iamEnsureInstanceProfileArgs struct {
	Name    string   `arg:"positional,required"`
	Policy  []string `arg:"--policy" help:"policy name, can specify multiple values"`
	Allow   []string `arg:"--allow" help:"\"$service:$action $arn\", example: \"s3:* *\", can specify multiple values"`
	Preview bool     `arg:"-p,--preview"`
}

func (iamEnsureInstanceProfileArgs) Description() string {
	return "\nensure an iam instance-profile\n"
}

func iamEnsureInstanceProfile() {
	var args iamEnsureInstanceProfileArgs
	arg.MustParse(&args)
	ctx := context.Background()
	err := lib.IamEnsureInstanceProfile(ctx, "", args.Name, args.Policy, args.Allow, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
