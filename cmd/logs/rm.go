package libaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["logs-rm"] = logsRm
	lib.Args["logs-rm"] = logsRmArgs{}
}

type logsRmArgs struct {
	Name    string `arg:"positional,required"`
	Preview bool   `arg:"-p,--preview"`
}

func (logsRmArgs) Description() string {
	return "\nrm log group\n"
}

func logsRm() {
	var args logsRmArgs
	arg.MustParse(&args)
	ctx := context.Background()
	err := lib.LogsDeleteGroup(ctx, args.Name, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
