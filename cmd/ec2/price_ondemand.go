package libaws

import (
	"context"
	"fmt"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ec2-price-ondemand"] = ec2PriceOndemand
	lib.Args["ec2-price-ondemand"] = ec2PriceOndemandArgs{}
}

type ec2PriceOndemandArgs struct {
	InstanceType string `arg:"positional" help:"optional instance type, e.g. m6i.large"`
}

func (ec2PriceOndemandArgs) Description() string {
	return "\nshow EC2 on-demand prices\n"
}

func ec2PriceOndemand() {
	var args ec2PriceOndemandArgs
	arg.MustParse(&args)
	ctx := context.Background()
	region := lib.Region()
	prices, err := lib.EC2OnDemandPrices(ctx, region, args.InstanceType)
	if err != nil {
		lib.Logger.Println("error:", err)
		os.Exit(1)
	}
	if len(prices) == 0 {
		os.Exit(1)
	}
	if args.InstanceType != "" {
		price, ok := prices[args.InstanceType]
		if !ok {
			os.Exit(1)
		}
		fmt.Println(price)
		return
	}
	for _, t := range lib.SortedInstanceTypes(prices) {
		fmt.Printf("%s %g\n", t, prices[t])
	}
}
