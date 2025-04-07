package libaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ses-ensure-receipt-rule"] = sesEnsureReceiptRule
	lib.Args["ses-ensure-receipt-rule"] = sesEnsureReceiptRulesArg{}
}

type sesEnsureReceiptRulesArg struct {
	Domain    string `arg:"positional"`
	LambdaArn string `arg:"positional"`
	Bucket    string `arg:"positional"`
	Prefix    string `arg:"positional"`
	Preview   bool   `arg:"-p,--preview"`
}

func (sesEnsureReceiptRulesArg) Description() string {
	return "\n \n"
}

func sesEnsureReceiptRule() {
	var args sesEnsureReceiptRulesArg
	arg.MustParse(&args)
	ctx := context.Background()
	_, err := lib.SesEnsureReceiptRuleset(ctx, args.Domain, args.LambdaArn, args.Bucket, args.Prefix, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
