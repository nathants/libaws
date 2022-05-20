package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["lambda-rm"] = lambdaRm
	lib.Args["lambda-rm"] = lambdaRmArgs{}
}

type lambdaRmArgs struct {
	Name    string `arg:"positional,required"`
	Preview bool   `arg:"-p,--preview"`
}

func (lambdaRmArgs) Description() string {
	return "\nlambda rm\n"
}

func lambdaRm() {
	var args lambdaRmArgs
	arg.MustParse(&args)
	ctx := context.Background()
	err := lib.LambdaDelete(ctx, args.Name, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
