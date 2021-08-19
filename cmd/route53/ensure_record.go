package cliaws

import (
	"context"
	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["route53-ensure-record"] = route53EnsureRecord
}

type route53EnsureRecordArgs struct {
	ZoneName   string   `arg:"positional,required"`
	RecordName string   `arg:"positional,required"`
	Attrs      []string `arg:"positional,required"`
	Preview    bool     `arg:"-p,--preview"`
}

func (route53EnsureRecordArgs) Description() string {
	return `
ensure a route53 record

example:
 - cli-aws route53-ensure-record example.com domain.example.com TTL=60 Type=A Value=1.1.1.1 Value=2.2.2.2

required attrs:
 - TTL=VALUE
 - Type=VALUE
 - Value=VALUE

`
}

func route53EnsureRecord() {
	var args route53EnsureRecordArgs
	arg.MustParse(&args)
	ctx := context.Background()
	input, err := lib.Route53EnsureRecordInput(args.ZoneName, args.RecordName, args.Attrs)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	err = lib.Route53EnsureRecord(ctx, input, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
