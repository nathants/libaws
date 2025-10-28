package libaws

import (
	"context"
	"fmt"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["infra-url-func"] = infraUrlFunc
	lib.Args["infra-url-func"] = infraUrlFuncArgs{}
}

type infraUrlFuncArgs struct {
	YamlPath   string `arg:"positional,required"`
	LambdaName string `arg:"positional,required"`
}

func (infraUrlFuncArgs) Description() string {
	return "\nget infra function url\n"
}

func infraUrlFunc() {
	var args infraUrlFuncArgs
	arg.MustParse(&args)

	ctx := context.Background()

	infraSet, err := lib.InfraParse(args.YamlPath)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}

	for name := range infraSet.Lambda {
		if name == args.LambdaName {
			url, err := lib.FuncUrl(ctx, name)
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}
			fmt.Println(url)
			os.Exit(0)
		}
	}
	os.Exit(1)
}
