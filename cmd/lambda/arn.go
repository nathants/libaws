package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["lambda-arn"] = lambdaArn
	lib.Args["lambda-arn"] = lambdaArnArgs{}
}

type lambdaArnArgs struct {
	Path string `arg:"positional,required"`
}

func (lambdaArnArgs) Description() string {
	return "\nget lambda arn\n"
}

func lambdaArn() {
	var args lambdaArnArgs
	arg.MustParse(&args)
	ctx := context.Background()
	name, err := lib.LambdaName(args.Path)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	arn, err := lib.LambdaArn(ctx, name)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(arn)
}
