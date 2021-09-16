package cliaws

import (
	"context"
	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["route53-ensure-zone"] = route53EnsureZone
lib.Args["route53-ensure-zone"] = route53EnsureZoneArgs{}
}

type route53EnsureZoneArgs struct {
	Name    string `arg:"positional,required"`
	Preview bool   `arg:"-p,--preview"`
}

func (route53EnsureZoneArgs) Description() string {
	return "\nensure hosted zone\n"
}

func route53EnsureZone() {
	var args route53EnsureZoneArgs
	arg.MustParse(&args)
	ctx := context.Background()
	err := lib.Route53EnsureZone(ctx, args.Name, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
