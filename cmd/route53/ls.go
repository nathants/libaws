package cliaws

import (
	"context"
	"fmt"
	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
	"strings"
)

func init() {
	lib.Commands["route53-ls"] = route53Ls
	lib.Args["route53-ls"] = route53LsArgs{}
}

type route53LsArgs struct {
}

func (route53LsArgs) Description() string {
	return "\nlist route53 entries\n"
}

func route53Ls() {
	var args route53LsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	zones, err := lib.Route53ListZones(ctx)
	if err != nil {
		lib.Logger.Fatal(err)
	}
	for _, zone := range zones {
		fmt.Println(*zone.Name)
		records, err := lib.Route53ListRecords(ctx, *zone.Id)
		if err != nil {
			lib.Logger.Fatal(err)
		}
		for _, record := range records {
			if record.AliasTarget != nil {
				fmt.Println("-", *record.Name, "Alias =>", *record.AliasTarget.DNSName)
			} else {
				vals := []string{}
				for _, val := range record.ResourceRecords {
					vals = append(vals, *val.Value)
				}
				fmt.Println("-", *record.Name, *record.Type, *record.TTL, "=>", strings.Join(vals, " "))
			}
		}
		fmt.Println()
	}
}
