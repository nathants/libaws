package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["lambda-vars"] = lambdaVars
	lib.Args["lambda-vars"] = lambdaVarsArgs{}
}

type lambdaVarsArgs struct {
	Path string `arg:"positional"`
}

func (lambdaVarsArgs) Description() string {
	return "\nget lambda vars\n"
}

func lambdaVars() {
	var args lambdaVarsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	name, err := lib.LambdaName(args.Path)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	out, err := lib.LambdaClient().GetFunctionWithContext(ctx, &lambda.GetFunctionInput{
		FunctionName: aws.String(name),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for k, v := range out.Configuration.Environment.Variables {
		if v != nil {
			fmt.Printf("%s=%s\n", k, *v)
		}
	}
}
