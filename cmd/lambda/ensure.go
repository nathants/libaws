package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["lambda-ensure"] = lambdaEnsure
	lib.Args["lambda-ensure"] = lambdaEnsureArgs{}
}

type lambdaEnsureArgs struct {
	Path    string `arg:"positional,required"`
	Preview bool   `arg:"-p,--preview"`
	Quick   bool   `arg:"-q,--quick" help:"quickly patch lambda code without updating anything else"`
}

func (lambdaEnsureArgs) Description() string {
	return "\nlambda ensure\n"
}

func lambdaEnsure() {
	var args lambdaEnsureArgs
	arg.MustParse(&args)
	ctx := context.Background()
	err := lib.LambdaEnsure(ctx, args.Path, args.Quick, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error:", err)
	}
}
