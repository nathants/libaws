package libaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["iam-ensure-role"] = iamEnsureRole
	lib.Args["iam-ensure-role"] = iamEnsureRoleArgs{}
}

type iamEnsureRoleArgs struct {
	Name      string   `arg:"positional,required"`
	Principal string   `arg:"positional,required"`
	Policy    []string `arg:"--policy" help:"policy name, can specify multiple values"`
	Allow     []string `arg:"--allow" help:"\"$service:$action $arn\", example: \"s3:* *\", can specify multiple values"`
	Preview   bool     `arg:"-p,--preview"`
}

func (iamEnsureRoleArgs) Description() string {
	return "\nensure an iam role\n"
}

func iamEnsureRole() {
	var args iamEnsureRoleArgs
	arg.MustParse(&args)
	ctx := context.Background()
	err := lib.IamEnsureRole(ctx, "", args.Name, args.Principal, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	err = lib.IamEnsureRolePolicies(ctx, args.Name, args.Policy, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	err = lib.IamEnsureRoleAllows(ctx, args.Name, args.Allow, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
