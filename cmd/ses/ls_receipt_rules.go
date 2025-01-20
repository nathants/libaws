package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/service/ses"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ses-ls-receipt-rules"] = sesLsReceiptRules
	lib.Args["ses-ls-receipt-rules"] = sesLsReceiptRulesArgs{}
}

type sesLsReceiptRulesArgs struct {
}

func (sesLsReceiptRulesArgs) Description() string {
	return "\n \n"
}

func sesLsReceiptRules() {
	var args sesLsReceiptRulesArgs
	arg.MustParse(&args)
	ctx := context.Background()
	rules, err := lib.SesListReceiptRulesets(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, rule := range rules {
		fmt.Println()
		fmt.Println(*rule.Name)
		out, err := lib.SesClient().DescribeReceiptRuleSet(&ses.DescribeReceiptRuleSetInput{
			RuleSetName: rule.Name,
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		fmt.Println(lib.PformatAlways(out))
	}
}
