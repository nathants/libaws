package cliaws

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["logs-tail"] = logsTail
	lib.Args["logs-tail"] = logsTailArgs{}
}

type logsTailArgs struct {
	Name       string `arg:"positional,required"`
	FromHours  int    `arg:"-f,--from-hours" default:"0" help:"get data no older than this"`
	Timestamps bool   `arg:"-t,--timestamps" help:"show timestamps"`
	ExitAfter  string `arg:"-e,--exit-after" help:"when tailing, after this substring is seen in a log line, exit"`
}

func (logsTailArgs) Description() string {
	return "\ntail logs\n"
}

func logsTail() {
	var args logsTailArgs
	arg.MustParse(&args)
	ctx := context.Background()
	minAge := time.Now().Add(-1 * time.Hour * time.Duration(args.FromHours))
	err := lib.LogsTail(ctx, args.Name, minAge, func(timestamp time.Time, line string) {
		if args.Timestamps {
			fmt.Println(timestamp, line)
		} else {
			fmt.Println(line)
		}
		if args.ExitAfter != "" && strings.Contains(line, args.ExitAfter) {
			os.Exit(0)
		}
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
