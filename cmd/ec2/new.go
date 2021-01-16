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
	Name         string   `arg:"positional,required"`
	Num          int      `arg:"-n,--num" default:"1"`
	Type         string   `arg:"-t,--type,required"`
	Ami          string   `arg:"-a,--ami,required"`
	UserName     string   `arg:"-u,--user,required" help:"ssh user name"`
	Key          string   `arg:"-k,--key,required"`
	SpotStrategy string   `arg:"-s,--spot" help:"if unspecified create onDemand instances, otherwise choose spotStrategy: lowestPrice|diversified|capacityOptimized"`
	SgID         string   `arg:"--sg,required"`
	SubnetIds    []string `arg:"--subnets,required"`
	Gigs         int      `arg:"-g,--gigs" default:"16"`
	Iops         int      `arg:"--iops" help:"gp3 iops" default:"3000"`
	Throughput   int      `arg:"--throughput" help:"gp3 throughput mb/s" default:"125"`
	Init         string   `arg:"-i,--init,required" help:"cloud init bash script"`
	Tags         []string `arg:"--tags" help:"key=value"`
	Profile      string   `arg:"-p,--profile,required" help:"iam instance profile name"`
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
	fleetConfig := &lib.EC2FleetConfig{
		NumInstances: args.Num,
		AmiID:        args.Ami,
		UserName:     args.UserName,
		InstanceType: args.Type,
		Name:         args.Name,
		Key:          args.Key,
		SgID:         args.SgID,
		SubnetIds:    args.SubnetIds,
		Gigs:         args.Gigs,
		Iops:         args.Iops,
		Throughput:   args.Throughput,
		Init:         args.Init,
		Tags:         tags,
		Profile:      args.Profile,
	}
	var err error
	if args.SpotStrategy != "" {
		instances, err = lib.EC2RequestSpotFleet(ctx, args.SpotStrategy, fleetConfig)
	} else {
		instances, err = lib.EC2NewInstances(ctx, fleetConfig)
	}
	if err != nil {
		lib.Logger.Fatal("error:", err)
	}
	for _, instance := range instances {
		fmt.Println(*instance.InstanceId)
	}
}
