package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["lambda-api"] = lambdaApi
	lib.Args["lambda-api"] = lambdaApiArgs{}
}

type lambdaApiArgs struct {
	Path string `arg:"positional"`
}

func (lambdaApiArgs) Description() string {
	return "\nget lambda api\n"
}

func lambdaApi() {
	var args lambdaApiArgs
	arg.MustParse(&args)
	ctx := context.Background()
	name, err := lib.LambdaName(args.Path)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	url, err := lib.ApiUrl(ctx, name)
	if err != nil {
	    lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(url)
}
