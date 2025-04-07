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
	Name string `arg:"positional"`
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
	for k, v := range out.Configuration.Environment.Variables {
		fmt.Printf("%s=%s\n", k, v)
	}
}
