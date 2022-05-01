package cliaws

import (
	"context"
	"fmt"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
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
		records, err := lib.Route53ListRecords(ctx, *zone.Id)
		if err != nil {
			lib.Logger.Fatal(err)
		}
		for _, record := range records {
			if record.AliasTarget != nil {
				fmt.Println(strings.TrimRight(*zone.Name, "."), strings.TrimRight(*record.Name, "."), "Type=Alias", "Value="+*record.AliasTarget.DNSName, "HostedZoneId="+*record.AliasTarget.HostedZoneId)
			} else {
				vals := []string{}
				for _, val := range record.ResourceRecords {
					if strings.Contains(*val.Value, " ") || strings.Contains(*val.Value, `"`) {
						vals = append(vals, "'Value="+*val.Value+"'")
					} else {
						vals = append(vals, "Value="+*val.Value)
					}
				}
				fmt.Println(strings.TrimRight(*zone.Name, "."), strings.TrimRight(*record.Name, "."), "TTL="+fmt.Sprint(*record.TTL), "Type="+*record.Type, strings.Join(vals, " "))
			}
		}
	}
}
