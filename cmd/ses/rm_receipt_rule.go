package libaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ses-rm-receipt-rule"] = sesRmReceiptRule
	lib.Args["ses-rm-receipt-rule"] = sesRmReceiptRulesArg{}
}

type sesRmReceiptRulesArg struct {
	Domain  string `arg:"positional"`
	Preview bool   `arg:"-p,--preview"`
}

func (sesRmReceiptRulesArg) Description() string {
	return "\n \n"
}

func sesRmReceiptRule() {
	var args sesRmReceiptRulesArg
	arg.MustParse(&args)
	ctx := context.Background()
	err := lib.SesRmReceiptRuleset(ctx, args.Domain, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
