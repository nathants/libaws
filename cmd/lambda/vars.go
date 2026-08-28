package libaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["lambda-vars"] = lambdaVars
	lib.Args["lambda-vars"] = lambdaVarsArgs{}
}

type lambdaVarsArgs struct {
	Name             string `arg:"positional,required"`
	ShowEnvVarValues bool   `arg:"-v,--env-values" help:"show environment variable values instead of their hash"`
}

func (lambdaVarsArgs) Description() string {
	return "\nget lambda vars\n"
}

func lambdaVars() {
	var args lambdaVarsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	out, err := lib.LambdaClient().GetFunction(ctx, &lambda.GetFunctionInput{
		FunctionName: aws.String(args.Name),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	variables := map[string]string{}
	if out.Configuration != nil && out.Configuration.Environment != nil {
		variables = out.Configuration.Environment.Variables
	}
	for _, line := range formatLambdaVariables(variables, args.ShowEnvVarValues) {
		fmt.Println(line)
	}
}
