package libaws

import (
	"context"
	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["route53-rm-record"] = route53DeleteRecord
	lib.Args["route53-rm-record"] = route53DeleteRecordArgs{}
}

type route53DeleteRecordArgs struct {
	ZoneName   string   `arg:"positional,required"`
	RecordName string   `arg:"positional,required"`
	Attr       []string `arg:"positional,required"`
	Preview    bool     `arg:"-p,--preview"`
}

func (route53DeleteRecordArgs) Description() string {
	return `delete a route53 record

example:
 - libaws route53-rm-record example.com domain.example.com Type=A TTL=60 Value=1.1.1.1 Value=2.2.2.2

required attrs:
 - TTL=VALUE
 - Type=VALUE
 - Value=VALUE
`
}

func route53DeleteRecord() {
	var args route53DeleteRecordArgs
	arg.MustParse(&args)
	ctx := context.Background()
	input, err := lib.Route53EnsureRecordInput(args.ZoneName, args.RecordName, args.Attr)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	err = lib.Route53DeleteRecord(ctx, input, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
