package libaws

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
	Quick            string `arg:"-q,--quick" help:"patch this Lambda's code and reconcile its runtime configuration only"`
	ShowEnvVarValues bool   `arg:"-v,--env-values" help:"show environment variable values instead of their hash"`
	BuildOnly        string `arg:"--build-only" help:"build required Lambda artifacts into this directory and stop; never accesses AWS"`
	FromBuild        string `arg:"--from-build" help:"deploy using only hash-verified Lambda artifacts from this directory; never builds"`
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
	if err := lib.ValidateInfraBuildMode(args.BuildOnly, args.FromBuild, args.Preview); err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	switch {
	case args.BuildOnly != "":
		err = lib.InfraBuildOnly(infraSet, args.Quick, args.BuildOnly)
	case args.FromBuild != "":
		err = lib.InfraEnsureWithArtifacts(ctx, infraSet, args.Quick, args.Preview, args.ShowEnvVarValues, args.FromBuild)
	default:
		err = lib.InfraEnsure(ctx, infraSet, args.Quick, args.Preview, args.ShowEnvVarValues)
	}
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
