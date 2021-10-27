package cliaws

import (
	// "fmt"
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"

)

func init() {
	lib.Commands["lambda-ensure"] = lambdaEnsure
	lib.Args["lambda-ensure"] = lambdaEnsureArgs{}
}

type lambdaEnsureArgs struct {
	Path string `arg:"positional"`
}

func (lambdaEnsureArgs) Description() string {
	return "\nlambda ensure\n"
}

func lambdaEnsure() {
	var args lambdaEnsureArgs
	arg.MustParse(&args)
	ctx := context.Background()
	_ = ctx
}
