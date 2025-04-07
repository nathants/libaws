package libaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["cloudwatch-ls-alarms"] = cloudwatchLsAlarms
	lib.Args["cloudwatch-ls-alarms"] = cloudwatchLsAlarmsArgs{}
}

type cloudwatchLsAlarmsArgs struct {
}

func (cloudwatchLsAlarmsArgs) Description() string {
	return "\nlist cloudwatch alarms\n"
}

func cloudwatchLsAlarms() {
	var args cloudwatchLsAlarmsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	alarms, err := lib.CloudwatchListAlarms(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, alarm := range alarms {
		fmt.Println(lib.Pformat(alarm))
	}
}
