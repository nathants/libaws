package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["events-ls-rules"] = eventsLsRules
	lib.Args["events-ls-rules"] = eventsLsRulesArgs{}
}

type eventsLsRulesArgs struct {
	BusName string `arg:"positional" help:"specify name to target a bus other than the default bus"`
}

func (eventsLsRulesArgs) Description() string {
	return "\nlist events rules\n"
}

func eventsLsRules() {
	var args eventsLsRulesArgs
	arg.MustParse(&args)
	var busName *string
	if args.BusName != "" {
		busName = aws.String(args.BusName)
	}
	ctx := context.Background()
	rules, err := lib.EventsListRules(ctx, busName)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, rule := range rules {
		fmt.Println(lib.Pformat(rule))
	}
}
