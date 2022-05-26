package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["infra-ensure"] = infraEnsure
	lib.Args["infra-ensure"] = infraEnsureArgs{}
}

type infraEnsureArgs struct {
	YamlPath         string `arg:"positional,required"`
	Preview          bool   `arg:"-p,--preview"`
	Quick            string `arg:"-q,--quick" help:"patch this lambda's code without updating infrastructure"`
	ShowEnvVarValues bool   `arg:"-v,--env-values" help:"show environment variable values instead of their hash"`
}

func (infraEnsureArgs) Description() string {
	return "\nensure infra\nsee "
}

func infraEnsure() {
	var args infraEnsureArgs
	arg.MustParse(&args)
	ctx := context.Background()
	infraSet, err := lib.InfraParse(args.YamlPath)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	err = lib.InfraEnsure(ctx, infraSet, args.Quick, args.Preview, args.ShowEnvVarValues)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
