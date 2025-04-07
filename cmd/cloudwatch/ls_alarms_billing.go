package libaws

import (
	"context"
	"fmt"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["cloudwatch-ls-alarms-billing"] = cloudwatchLsAlarmsBilling
	lib.Args["cloudwatch-ls-alarms-billing"] = cloudwatchLsAlarmsBillingArgs{}
}

type cloudwatchLsAlarmsBillingArgs struct {
}

func (cloudwatchLsAlarmsBillingArgs) Description() string {
	return "\nlist cloudwatch billing alarms in us-east-1\n"
}

func cloudwatchLsAlarmsBilling() {
	var args cloudwatchLsAlarmsBillingArgs
	arg.MustParse(&args)
	ctx := context.Background()
	err := os.Setenv("AWS_DEFAULT_REGION", "us-east-1")
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	out, err := lib.CloudwatchListAlarms(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, alarm := range out {
		if *alarm.Namespace == "AWS/Billing" {
			fmt.Println(lib.Pformat(alarm))
		}
	}
}
