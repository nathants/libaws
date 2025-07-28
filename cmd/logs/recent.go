package libaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["logs-recent"] = logsRecent
	lib.Args["logs-recent"] = logsRecentArgs{}
}

type logsRecentArgs struct {
	Name     string `arg:"positional,required"`
	NumLines int    `arg:"positional,required" help:"number of recent log lines to show"`
}

func (logsRecentArgs) Description() string {
	return "\nshow the N most recent log lines\n"
}

func logsRecent() {
	var args logsRecentArgs
	arg.MustParse(&args)
	ctx := context.Background()
	lines, err := lib.LogsRecent(ctx, args.Name, args.NumLines)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, line := range lines {
		fmt.Println(line)
	}
}
