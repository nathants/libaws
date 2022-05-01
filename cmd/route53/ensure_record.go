package cliaws

import (
	"context"
	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["route53-ensure-record"] = route53EnsureRecord
	lib.Args["route53-ensure-record"] = route53EnsureRecordArgs{}
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

examples:
 - cli-aws route53-ensure-record example.com example.com Type=A TTL=60 Value=1.1.1.1 Value=2.2.2.2
 - cli-aws route53-ensure-record example.com cname.example.com Type=CNAME TTL=60 Value=about.us-west-2.domain.example.com
 - cli-aws route53-ensure-record example.com alias.example.com Type=Alias Value=d-XXX.execute-api.us-west-2.amazonaws.com HostedZoneId=XXX

required attrs for standard dns records:
 - TTL=VALUE
 - Type=VALUE
 - Value=VALUE

required attrs for aws alias records:
 - Type=Alias
 - Value=VALUE
 - HostedZoneId=VALUE

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
