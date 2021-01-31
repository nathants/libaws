package cliaws

import (
	"context"
	"fmt"
	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/nathants/cli-aws/lib"
	"math/rand"
	"strings"
)

func init() {
	lib.Commands["ec2-new"] = ec2New
}

type newArgs struct {
	Name           string   `arg:"positional,required"`
	Num            int      `arg:"-n,--num" default:"1"`
	Type           string   `arg:"-t,--type,required"`
	Ami            string   `arg:"-a,--ami,required" help:"ami-ID | arch | amzn | lambda | deeplearning | bionic | xenial | trusty | eoan | focal"`
	UserName       string   `arg:"-u,--user" help:"ssh user name, otherwise look for 'user' tag on instance or find via ami name lookup"`
	Key            string   `arg:"-k,--key,required"`
	SpotStrategy   string   `arg:"-s,--spot" help:"leave unspecified to create onDemand instances.\n                         otherwise choose spotStrategy from: lowestPrice | diversified | capacityOptimized"`
	Sg             string   `arg:"--sg,required" help:"security group name or id"`
	SubnetIds      []string `arg:"--subnets" help:"subnet-ids as space separated values. specify instead of --vpc"`
	Vpc            string   `arg:"-v,--vpc" help:"vpc name or id. specify instead of --subnet-ids"`
	Gigs           int      `arg:"-g,--gigs" help:"ebs gigabytes\n                        " default:"16"`
	Iops           int      `arg:"--iops" help:"gp3 iops\n                        " default:"3000"`
	Throughput     int      `arg:"--throughput" help:"gp3 throughput mb/s\n                        " default:"125"`
	Init           string   `arg:"-i,--init" help:"cloud init bash script"`
	Tags           []string `arg:"--tags" help:"space separated values like: key=value"`
	Profile        string   `arg:"-p,--profile" help:"iam instance profile name"`
	SecondsTimeout int      `arg:"--seconds-timeout" default:"3600" help:"will $(sudo poweroff) after this many seconds.\n                         calls $(bash /etc/timeout.sh) and waits 60 seconds for it to exit before calling $(sudo poweroff).\n                         set to 0 to disable.\n                         $(sudo journalctl -f -u timeout.service) to follow logs.\n                        "`
}

func (newArgs) Description() string {
	return "\ncreate ec2 instances\n"
}

func useSubnetsFromVpc(ctx context.Context, args *newArgs) {
	if args.Vpc != "" {
		zones, err := lib.EC2ZonesWithInstance(ctx, args.Type)
		if err != nil {
			lib.Logger.Fatal("error:", err)
		}
		vpcID := args.Vpc
		if !strings.HasPrefix("vpc-", args.Vpc) {
			vpcID, err = lib.VpcID(ctx, args.Vpc)
			if err != nil {
				lib.Logger.Fatal("error:", err)
			}
		}
		subnets, err := lib.VpcSubnets(ctx, vpcID)
		if err != nil {
			lib.Logger.Fatal("error:", err)
		}
		if args.SpotStrategy == "" {
			zone := zones[rand.Intn(len(zones))]
			for _, subnet := range subnets {
				if *subnet.AvailabilityZone == zone {
					args.SubnetIds = []string{*subnet.SubnetId}
					break
				}
			}
			if len(args.SubnetIds) != 1 {
				lib.Logger.Fatalf("no subnet in zone %s for vpc %s", zone, vpcID)
			}
		} else {
			for _, subnet := range subnets {
				args.SubnetIds = append(args.SubnetIds, *subnet.SubnetId)
			}
			if len(args.SubnetIds) == 0 {
				lib.Logger.Fatalf("no subnets for vpc %s", vpcID)
			}
		}
	}
}

func ec2New() {
	var args newArgs
	p := arg.MustParse(&args)
	ctx, cancel := context.WithCancel(context.Background())
	lib.SignalHandler(cancel)
	if args.Vpc == "" && len(args.SubnetIds) == 0 {
		p.Fail("you must specify one of --vpc | --subnets")
	}
	useSubnetsFromVpc(ctx, &args)
	if !strings.HasPrefix(args.Ami, "ami-") {
		ami, sshUser, err := lib.EC2Ami(ctx, args.Ami)
		if err != nil {
			lib.Logger.Fatal("error:", err)
		}
		args.Ami = ami
		args.UserName = sshUser
	}
	if !strings.HasPrefix(args.Sg, "sg-") {
		sgID, err := lib.EC2SgID(ctx, args.Sg)
		if err != nil {
			lib.Logger.Fatal("error:", err)
		}
		args.Sg = sgID
	}
	var instances []*ec2.Instance
	var tags []lib.EC2Tag
	for _, tag := range args.Tags {
		parts := strings.Split(tag, "=")
		tags = append(tags, lib.EC2Tag{
			Name:  parts[0],
			Value: parts[1],
		})
	}
	fleetConfig := &lib.EC2Config{
		NumInstances:   args.Num,
		AmiID:          args.Ami,
		UserName:       args.UserName,
		InstanceType:   args.Type,
		Name:           args.Name,
		Key:            args.Key,
		SgID:           args.Sg,
		SubnetIds:      args.SubnetIds,
		Gigs:           args.Gigs,
		Iops:           args.Iops,
		Throughput:     args.Throughput,
		Init:           args.Init,
		Tags:           tags,
		Profile:        args.Profile,
		SecondsTimeout: args.SecondsTimeout,
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
