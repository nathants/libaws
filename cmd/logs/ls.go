package cliaws

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/dustin/go-humanize"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["logs-ls"] = logsLs
	lib.Args["logs-ls"] = logsLsArgs{}
}

type logsLsArgs struct {
}

func (logsLsArgs) Description() string {
	return "\nlist logs\n"
}

func zeroOnNil(x *int64) int64 {
	if x == nil {
		return 0
	}
	return *x
}

func logsLs() {
	var args logsLsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	logs, err := lib.LogsListLogGroups(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	sort.Slice(logs, func(i, j int) bool { return *logs[i].StoredBytes < *logs[j].StoredBytes })
	for _, log := range logs {
		fmt.Println(
			*log.LogGroupName,
			fmt.Sprintf("retention-days=%d", zeroOnNil(log.RetentionInDays)),
			fmt.Sprintf("stored=%s", strings.ReplaceAll(humanize.Bytes(uint64(*log.StoredBytes)), " ", "")),
		)
	}
}
