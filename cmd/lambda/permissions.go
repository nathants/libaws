package cliaws

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["lambda-permissions"] = lambdaPermissions
	lib.Args["lambda-permissions"] = lambdaPermissionsArgs{}
}

type lambdaPermissionsArgs struct {
	Path string `arg:"positional"`
}

func (lambdaPermissionsArgs) Description() string {
	return "\nget lambda permissions\n"
}

func lambdaPermissions() {
	var args lambdaPermissionsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	name, err := lib.LambdaName(args.Path)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	out, err := lib.LambdaClient().GetPolicyWithContext(ctx, &lambda.GetPolicyInput{
		FunctionName: aws.String(name),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	val := map[string]interface{}{}
	err = json.Unmarshal([]byte(*out.Policy), &val)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(lib.PformatAlways(val))
}
