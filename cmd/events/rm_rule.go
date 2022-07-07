package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatchevents"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["events-rm-rule"] = eventsRmRule
	lib.Args["events-rm-rule"] = eventsRmRuleArgs{}
}

type eventsRmRuleArgs struct {
	RuleName string `arg:"positional,required"`
	BusName  string `arg:"positional" help:"specify name to target a bus other than the default bus"`
	Preview  bool   `arg:"-p,--preview"`
}

func (eventsRmRuleArgs) Description() string {
	return "\ndelete event rule\n"
}

func eventsRmRule() {
	var args eventsRmRuleArgs
	arg.MustParse(&args)
	var busName *string
	if args.BusName != "" {
		busName = aws.String(args.BusName)
	}
	ctx := context.Background()
	rule, err := lib.EventsClient().DescribeRuleWithContext(ctx, &cloudwatchevents.DescribeRuleInput{
		EventBusName: busName,
		Name:         aws.String(args.RuleName),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	targets, err := lib.EventsListRuleTargets(ctx, args.RuleName, busName)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	var ids []*string
	for _, target := range targets {
		fmt.Println(lib.PreviewString(args.Preview)+"delete target:", lib.Pformat(target))
		ids = append(ids, target.Id)
	}
	if !args.Preview {
		_, err = lib.EventsClient().RemoveTargetsWithContext(ctx, &cloudwatchevents.RemoveTargetsInput{
			EventBusName: busName,
			Rule:         aws.String(args.RuleName),
			Ids:          ids,
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
	}
	fmt.Println(lib.PreviewString(args.Preview)+"delete rule:", lib.Pformat(rule))
	if !args.Preview {
		_, err := lib.EventsClient().DeleteRuleWithContext(ctx, &cloudwatchevents.DeleteRuleInput{
			EventBusName: busName,
			Name:         aws.String(args.RuleName),
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
	}
}
