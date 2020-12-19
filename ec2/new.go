package ec2

import (
	"context"
	"fmt"
	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ec2-new"] = ec2New
}

type newArgs struct {
	Name      string   `arg:"positional"`
	Num       int      `arg:"-n,--num" default:"1"`
	Type      string   `arg:"-t,--type"`
	Ami       string   `arg:"-a,--ami"`
	Key       string   `arg:"-k,--key"`
	SgID      string   `arg:"--sg"`
	SubnetIds []string `arg:"--subnets"`
	Gigs      int      `arg:"-g,--gigs" default:"16"`
}

func (newArgs) Description() string {
	return "\ncreate ec2 instances\n"
}

func ec2New() {
	var args newArgs
	arg.MustParse(&args)
	ctx, cancel := context.WithCancel(context.Background())
	lib.SignalHandler(cancel)
	fleet, err := lib.RequestSpotFleet(ctx, &lib.FleetConfig{
		NumInstances:  args.Num,
		AmiID:         args.Ami,
		InstanceTypes: []string{args.Type},
		Name:          args.Name,
		Key:           args.Key,
		SgID:          args.SgID,
		SubnetIds:     args.SubnetIds,
		Gigs:          args.Gigs,
	})
	if err != nil {
		lib.Logger.Fatalf("error: %s\n", err)
	}
	var instanceIDs []string
	fleetInstances, err := lib.RetryDescribeSpotFleetActiveInstances(ctx, fleet.SpotFleetRequestId)
	if err != nil {
		lib.Logger.Fatalf("error: %s\n", err)
	}
	for _, instance := range fleetInstances {
		instanceIDs = append(instanceIDs, *instance.InstanceId)
	}
	instances, err := lib.RetryDescribeInstances(ctx, instanceIDs)
	if err != nil {
		lib.Logger.Fatalf("error: %s\n", err)
	}
	for _, instance := range instances {
		fmt.Println(*instance.InstanceId)
	}
}
