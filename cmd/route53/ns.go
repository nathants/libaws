package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["route53-ns"] = route53NS
	lib.Args["route53-ns"] = route53NSArgs{}
}

type route53NSArgs struct {
	ZoneName string `arg:"positional,required"`
}

func (route53NSArgs) Description() string {
	return "\nlist name servers for zone\n"
}

func route53NS() {
	var args route53NSArgs
	arg.MustParse(&args)
	ctx := context.Background()
	id, err := lib.Route53ZoneID(ctx, args.ZoneName)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	out, err := lib.Route53Client().GetHostedZoneWithContext(ctx, &route53.GetHostedZoneInput{
		Id: aws.String(id),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, ns := range out.DelegationSet.NameServers {
		fmt.Println(*ns)
	}
}
