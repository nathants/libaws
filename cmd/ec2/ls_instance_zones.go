package cliaws

import (
	"context"
	"fmt"
	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ec2-ls-instance-zones"] = ec2LsInstanceZones
}

type lsInstanceZonesArgs struct {
	Type string `arg:"positional,required" help:"instance type"`
}

func (lsInstanceZonesArgs) Description() string {
	return "\nlist zones which support this instance type\n"
}

func ec2LsInstanceZones() {
	var args lsInstanceZonesArgs
	arg.MustParse(&args)
	ctx := context.Background()
	zones, err := lib.EC2ZonesWithInstance(ctx, args.Type)
	if err != nil {
		lib.Logger.Fatal("error:", err)
	}
	for _, zone := range zones {
		fmt.Println(zone)
	}
}
