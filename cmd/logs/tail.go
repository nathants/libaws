package cliaws

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["logs-tail"] = logsTail
	lib.Args["logs-tail"] = logsTailArgs{}
}

type logsTailArgs struct {
	Name       string `arg:"positional,required"`
	FromHours  int    `arg:"-f,--from-hours" default:"0" help:"get data no older than this"`
	Timestamps bool   `arg:"-t,--timestamps" help:"show timestamps"`
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
		parts := strings.Split(line, " ")
		if len(parts) > 0 && lib.Contains([]string{"START", "REPORT", "END"}, parts[0]) {
			return // skip lambda default logs
		}
		if args.Timestamps {
			fmt.Println(timestamp, line)
		} else {
			fmt.Println(line)
		}
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
