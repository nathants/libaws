package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
	"gopkg.in/yaml.v3"
)

func init() {
	lib.Commands["infra-ls"] = infraLs
	lib.Args["infra-ls"] = infraLsArgs{}
}

type infraLsArgs struct {
	Filter string `arg:"positional" help:"filter by name substring"`
}

func (infraLsArgs) Description() string {
	return "\nls infra\n"
}

func infraLs() {
	var args infraLsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	infra, err := lib.InfraList(ctx, args.Filter)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	bytes, err := yaml.Marshal(infra)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(string(bytes))
}
