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
	lib.Commands["lambda-describe"] = lambdaDescribe
	lib.Args["lambda-describe"] = lambdaDescribeArgs{}
}

type lambdaDescribeArgs struct {
	Name string `arg:"positional,required"`
}

func (lambdaDescribeArgs) Description() string {
	return "\nget lambda describe\n"
}

func lambdaDescribe() {
	var args lambdaDescribeArgs
	arg.MustParse(&args)
	ctx := context.Background()
	out, err := lib.LambdaClient().GetFunction(ctx, &lambda.GetFunctionInput{
		FunctionName: aws.String(args.Name),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(lib.PformatAlways(out))
	confOut, err := lib.LambdaClient().GetFunctionConfiguration(ctx, &lambda.GetFunctionConfigurationInput{
		FunctionName: aws.String(args.Name),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(lib.PformatAlways(confOut))
}
