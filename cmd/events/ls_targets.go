package libaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["events-ls-targets"] = eventsLsTargets
	lib.Args["events-ls-targets"] = eventsLsTargetsArgs{}
}

type eventsLsTargetsArgs struct {
	RuleName string `arg:"positional,required"`
	BusName  string `arg:"positional" default:"" help:"specify name to target a bus other than the default bus"`
}

func (eventsLsTargetsArgs) Description() string {
	return "\nlist event targets\n"
}

func eventsLsTargets() {
	var args eventsLsTargetsArgs
	arg.MustParse(&args)
	var busName *string
	if args.BusName != "" {
		busName = aws.String(args.BusName)
	}
	ctx := context.Background()
	targets, err := lib.EventsListRuleTargets(ctx, args.RuleName, busName)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, target := range targets {
		fmt.Println(lib.Pformat(target))
	}
}
