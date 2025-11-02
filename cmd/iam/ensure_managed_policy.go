package libaws

import (
	"context"
	"io"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["iam-ensure-managed-policy"] = iamEnsureManagedPolicy
	lib.Args["iam-ensure-managed-policy"] = iamEnsureManagedPolicyArgs{}
}

type iamEnsureManagedPolicyArgs struct {
	Name              string `arg:"positional,required"`
	PolicyDescription string `arg:"--description,required"`
	Document          string `arg:"--document,required" help:"inline JSON policy document or path to file"`
	Preview           bool   `arg:"-p,--preview"`
}

func (iamEnsureManagedPolicyArgs) Description() string {
	return "\nensure a managed iam policy exists with the given document and description\n"
}

func iamEnsureManagedPolicy() {
	var args iamEnsureManagedPolicyArgs
	arg.MustParse(&args)
	ctx := context.Background()

	policyDoc := args.Document
	if args.Document == "-" {
		bytes, err := io.ReadAll(os.Stdin)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		policyDoc = string(bytes)
	} else if lib.Exists(args.Document) {
		data, err := os.ReadFile(args.Document)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		policyDoc = string(data)
	}

	_, err := lib.IamEnsureManagedPolicy(ctx, args.Name, args.PolicyDescription, policyDoc, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
