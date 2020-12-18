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
	Type      string   `arg:"-t,--type""`
	Ami       string   `arg:"-a,--ami"`
	Key       string   `arg:"-k,--key"`
	SgID      string   `arg:"--sg"`
	SubnetIds []string `arg:"--subnets"`
}

func (newArgs) Description() string {
	return "\ncreate ec2 instances\n"
}

func ec2New() {
	var args newArgs
	arg.MustParse(&args)
	ctx := context.Background()
	fleet := lib.RequestSpotFleet(ctx, &lib.FleetConfig{
		NumInstances:  args.Num,
		AmiID:         args.Ami,
		InstanceTypes: []string{args.Type},
		Name:          args.Name,
		Key:           args.Key,
		SgID:          args.SgID,
		SubnetIds:     args.SubnetIds,
	})
	var instanceIDs []string
	for instance := range lib.RetryDescribeSpotFleetInstances(ctx, fleet.SpotFleetRequestId) {
		instanceIDs = append(instanceIDs, *instance.InstanceId)
	}
	instances := lib.RetryDescribeInstances(ctx, instanceIDs)
	for _, instance := range instances {
		fmt.Println(*instance.InstanceId)
	}
}
