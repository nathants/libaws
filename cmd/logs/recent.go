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
	NumLines int    `arg:"positional,required" help:"maximum number of recent log lines to show (1-10000)"`
}

func (logsRecentArgs) Description() string {
	return "\nshow up to N recent log lines\n"
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
