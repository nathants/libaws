package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["api-ls"] = apiLs
	lib.Args["api-ls"] = apiLsArgs{}
}

type apiLsArgs struct {
}

func (apiLsArgs) Description() string {
	return "\nlist apis\n"
}

func apiLs() {
	var args apiLsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	apis, err := lib.ApiList(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, api := range apis {
		fmt.Println(*api.Name)
	}
}
