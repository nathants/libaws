package libaws

import (
	"context"
	"fmt"
	"github.com/alexflint/go-arg"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ec2-ls-instance-zones"] = ec2LsInstanceZones
	lib.Args["ec2-ls-instance-zones"] = ec2LsInstanceZonesArgs{}
}

type ec2LsInstanceZonesArgs struct {
	Type string `arg:"positional,required" help:"instance type"`
}

func (ec2LsInstanceZonesArgs) Description() string {
	return "\nlist zones which support this instance type\n"
}

func ec2LsInstanceZones() {
	var args ec2LsInstanceZonesArgs
	arg.MustParse(&args)
	ctx := context.Background()

	zones, err := lib.EC2ZonesWithInstance(ctx, ec2types.InstanceType(args.Type))
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, zone := range zones {
		fmt.Println(zone)
	}
}
