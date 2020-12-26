package cliaws

import (
	"context"
	"fmt"
	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/nathants/cli-aws/lib"
	"strings"
)

func init() {
	lib.Commands["ec2-new"] = ec2New
}

type newArgs struct {
	Name         string   `arg:"positional"`
	Num          int      `arg:"-n,--num" default:"1"`
	Type         string   `arg:"-t,--type"`
	Ami          string   `arg:"-a,--ami"`
	Key          string   `arg:"-k,--key"`
	SpotStrategy string   `arg:"-s,--spot" default:"" help:"lowestPrice|diversified|capacityOptimized"`
	SgID         string   `arg:"--sg"`
	SubnetIds    []string `arg:"--subnets"`
	Gigs         int      `arg:"-g,--gigs" default:"16"`
	Init         string   `arg:"-i,--init" default:"" help:"cloud init bash script"`
	Tags         []string `arg:"--tags" help:"key=value"`
}

func (newArgs) Description() string {
	return "\ncreate ec2 instances\n"
}

func ec2New() {
	var args newArgs
	arg.MustParse(&args)
	ctx, cancel := context.WithCancel(context.Background())
	lib.SignalHandler(cancel)
	var instances []*ec2.Instance
	var tags []lib.EC2Tag
	for _, tag := range args.Tags {
		parts := strings.Split(tag, "=")
		tags = append(tags, lib.EC2Tag{
			Name:  parts[0],
			Value: parts[1],
		})
	}
	var err error
	if args.SpotStrategy != "" {
		instances, err = lib.EC2RequestSpotFleet(ctx, args.SpotStrategy, &lib.EC2FleetConfig{
			NumInstances:  args.Num,
			AmiID:         args.Ami,
			InstanceTypes: []string{args.Type},
			Name:          args.Name,
			Key:           args.Key,
			SgID:          args.SgID,
			SubnetIds:     args.SubnetIds,
			Gigs:          args.Gigs,
			Init:          args.Init,
			Tags:          tags,
		})
	} else {
		panic("todo")
	}
	if err != nil {
		lib.Logger.Fatal("error:", err)
	}
	for _, instance := range instances {
		fmt.Println(*instance.InstanceId)
	}
}
