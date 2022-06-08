package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["lambda-set-var"] = lambdaSetVar
	lib.Args["lambda-set-var"] = lambdaSetVarArgs{}
}

type lambdaSetVarArgs struct {
	Name string `arg:"positional,required"`
	Val  string `arg:"positional" help:"KEY=VALUE"`
}

func (lambdaSetVarArgs) Description() string {
	return "\nset lambda env var\n"
}

func lambdaSetVar() {
	var args lambdaSetVarArgs
	arg.MustParse(&args)
	ctx := context.Background()
	out, err := lib.LambdaClient().GetFunctionWithContext(ctx, &lambda.GetFunctionInput{
		FunctionName: aws.String(args.Name),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	k, v, err := lib.SplitOnce(args.Val, "=")
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	out.Configuration.Environment.Variables[k] = aws.String(v)
	_, err = lib.LambdaClient().UpdateFunctionConfigurationWithContext(ctx, &lambda.UpdateFunctionConfigurationInput{
		FunctionName: aws.String(args.Name),
		Environment: &lambda.Environment{
			Variables: out.Configuration.Environment.Variables,
		},
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
