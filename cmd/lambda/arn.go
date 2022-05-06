package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["lambda-arn"] = lambdaArn
	lib.Args["lambda-arn"] = lambdaArnArgs{}
}

type lambdaArnArgs struct {
	Name string `arg:"positional,required"`
}

func (lambdaArnArgs) Description() string {
	return "\nget lambda arn\n"
}

func lambdaArn() {
	var args lambdaArnArgs
	arg.MustParse(&args)
	ctx := context.Background()
	arn, err := lib.LambdaArn(ctx, args.Name)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(arn)
}
