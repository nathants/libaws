package libaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
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
	out, err := lib.LambdaClient().GetFunction(ctx, &lambda.GetFunctionInput{
		FunctionName: aws.String(args.Name),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	k, v, err := lib.SplitOnce(args.Val, "=")
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	out.Configuration.Environment.Variables[k] = v
	_, err = lib.LambdaClient().UpdateFunctionConfiguration(ctx, &lambda.UpdateFunctionConfigurationInput{
		FunctionName: aws.String(args.Name),
		Environment: &lambdatypes.Environment{
			Variables: out.Configuration.Environment.Variables,
		},
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
