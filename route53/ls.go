package route53

import (
	"context"
	"fmt"
	"strings"
	"github.com/nathants/cli-aws/lib"
	"github.com/alexflint/go-arg"
)

func init() {
	lib.Commands["route53-ls"] = route53Ls
}

type lsArgs struct {
}

func (lsArgs) Description() string {
	return "\nlist route53 entries\n"
}

func route53Ls() {
	var args lsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	for zone := range lib.Route53ListZones(ctx) {
		fmt.Println(*zone.Name)
		for record := range lib.Route53ListRecords(ctx, zone.Id) {
			if record.AliasTarget != nil {
				fmt.Println("-", *record.Name, "Alias =>", *record.AliasTarget.DNSName)
			} else {
				vals := []string{}
				for _, val := range record.ResourceRecords {
					vals = append(vals, *val.Value)
				}
				fmt.Println("-", *record.Name, record.Type, *record.TTL, "=>", strings.Join(vals, " "))
			}
		}
		fmt.Println()
	}
}
