package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["iam-ensure-instance-profile"] = iamEnsureInstanceProfile
	lib.Args["iam-ensure-instance-profile"] = iamEnsureInstanceProfileArgs{}
}

type iamEnsureInstanceProfileArgs struct {
	Name     string   `arg:"positional,required"`
	Policies []string `arg:"--policy"`
	Allows   []string `arg:"--allow"`
	Preview  bool     `arg:"-p,--preview"`
}

func (iamEnsureInstanceProfileArgs) Description() string {
	return "\nensure an iam instance-profile\n"
}

func iamEnsureInstanceProfile() {
	var args iamEnsureInstanceProfileArgs
	arg.MustParse(&args)
	ctx := context.Background()
	err := lib.IamEnsureInstanceProfile(ctx, args.Name, args.Policies, args.Allows, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
