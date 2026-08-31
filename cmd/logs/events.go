package libaws

import (
	"context"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["logs-events"] = logsEvents
	lib.Args["logs-events"] = logsEventsArgs{}
}

type logsEventsArgs struct {
	Name        string `arg:"positional,required" help:"log group name"`
	Filter      string `arg:"positional,required" help:"CloudWatch filter pattern"`
	StartMillis int64  `arg:"--start-millis,required" help:"inclusive event-time lower bound in UTC milliseconds"`
	EndMillis   *int64 `arg:"--end-millis" help:"optional exclusive event-time upper bound in UTC milliseconds"`
	MaxEvents   *int64 `arg:"--max-events" help:"optional maximum events; fail if the result exceeds it"`
}

func (logsEventsArgs) Description() string {
	return "\nstream exact filtered CloudWatch events as JSON lines\n"
}

func logsEvents() {
	var args logsEventsArgs
	arg.MustParse(&args)
	err := lib.LogsEvents(
		context.Background(),
		os.Stdout,
		args.Name,
		args.Filter,
		args.StartMillis,
		args.EndMillis,
		args.MaxEvents,
	)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
