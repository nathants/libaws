package cliaws

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/mattn/go-isatty"
	"github.com/nathants/cli-aws/lib"
	"github.com/tidwall/pretty"
)

func init() {
	lib.Commands["infra-ls"] = infraLs
	lib.Args["infra-ls"] = infraLsArgs{}
}

type infraLsArgs struct {
	Filter      string `arg:"positional" help:"filter by name substring"`
	SplitArrays bool   `arg:"-s,--split-spaces" help:"split arrays so values are one per line"`
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
	bytes, err := json.Marshal(infra)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	options := &pretty.Options{
		Indent: "    ",
	}
	if !args.SplitArrays {
		options.Width = 250
	}
	bytes = pretty.PrettyOptions(bytes, options)
	if isatty.IsTerminal(os.Stdout.Fd()) {
		bytes = pretty.Color(bytes, lib.PrettyStyle)
	}
	fmt.Println(string(bytes))
}
