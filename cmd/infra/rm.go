package libaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["infra-rm"] = infraRm
	lib.Args["infra-rm"] = infraRmArgs{}
}

type infraRmArgs struct {
	YamlPath string `arg:"positional,required"`
	Preview  bool   `arg:"-p,--preview"`
}

func (infraRmArgs) Description() string {
	return "\ninfra rm\n"
}

func infraRm() {
	var args infraRmArgs
	arg.MustParse(&args)
	ctx := context.Background()
	infraSet, err := lib.InfraParse(args.YamlPath)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	err = lib.InfraDelete(ctx, infraSet, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
