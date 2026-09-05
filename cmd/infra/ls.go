package libaws

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
	InfraSet         *string `arg:"--infraset" help:"list one exact infrastructure set by its ownership tag"`
	Filter           string  `arg:"positional" help:"filter by name substring"`
	ShowEnvVarValues bool    `arg:"-v,--env-values" help:"show environment variable values instead of their hash"`
}

func (infraLsArgs) Description() string {
	return "\nls infra\n"
}

func infraLs() {
	var args infraLsArgs
	parser := arg.MustParse(&args)
	if args.InfraSet != nil && args.Filter != "" {
		parser.Fail("--infraset and the substring filter are mutually exclusive")
	}
	ctx := context.Background()
	var infra *lib.InfraListOutput
	var err error
	if args.InfraSet != nil {
		infra, err = lib.InfraListSet(ctx, *args.InfraSet, args.ShowEnvVarValues)
	} else {
		infra, err = lib.InfraList(ctx, args.Filter, args.ShowEnvVarValues)
	}
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	bytes, err := yaml.Marshal(infra)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(string(bytes))
}
