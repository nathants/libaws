package libaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["vpc-ls-subnets"] = vpcLsSubnets
	lib.Args["vpc-ls-subnets"] = vpcLsSubnetsArgs{}
}

type vpcLsSubnetsArgs struct {
	Name string `arg:"positional,required"`
}

func (vpcLsSubnetsArgs) Description() string {
	return "\nls vpc subnets\n"
}

func vpcLsSubnets() {
	var args vpcLsSubnetsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	id, err := lib.VpcID(ctx, args.Name)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	subnets, err := lib.VpcListSubnets(ctx, id)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, subnet := range subnets {
		fmt.Println(*subnet.SubnetId, *subnet.AvailabilityZone)
	}
}
