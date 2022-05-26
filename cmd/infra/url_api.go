package cliaws

import (
	"context"
	"fmt"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["infra-url-api"] = infraUrlApi
	lib.Args["infra-url-api"] = infraUrlApiArgs{}
}

type infraUrlApiArgs struct {
	YamlPath   string `arg:"positional,required"`
	LambdaName string `arg:"positional,required"`
}

func (infraUrlApiArgs) Description() string {
	return "\nget infra api url\n"
}

func infraUrlApi() {
	var args infraUrlApiArgs
	arg.MustParse(&args)
	ctx := context.Background()
	infraSet, err := lib.InfraParse(args.YamlPath)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for name := range infraSet.Lambda {
		if name == args.LambdaName {
			url, err := lib.ApiUrl(ctx, name)
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}
			fmt.Println(url)
			os.Exit(0)
		}
	}
	os.Exit(1)
}
