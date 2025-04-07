package libaws

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ec2-new"] = ec2New
	lib.Args["ec2-new"] = ec2NewArgs{}
}

type ec2NewArgs struct {
	Name           string `arg:"positional,required"`
	Num            int    `arg:"-n,--num" default:"1"`
	Type           string `arg:"-t,--type,required"`
	Ami            string `arg:"-a,--ami,required" help:"ami-ID | amzn2 | amzn2023 | deeplearning | bionic | xenial | trusty | focal | jammy | bookworm | bullseye | buster | stretch | alpine-xx.yy.zz"`
	UserName       string `arg:"-u,--user" help:"ssh user name, otherwise look for 'user' tag on instance or find via ami name lookup"`
	Key            string `arg:"-k,--key,required"`
	EphemeralKey   bool   `arg:"-e,--ephemeral-key" help:"add an additional ssh keypair to this instance.\n                         the private key will be written to /tmp/libaws/SSH_ID/id_ed25519.\n                         the SSH_ID will be tagged on the instance."`
	SpotStrategy   string `arg:"-s,--spot" help:"leave unspecified to create on-demand instances.\n                         otherwise choose spotStrategy from: lowestPrice | diversified | capacityOptimized | capacityOptimizedPrioritized | priceCapacityOptimized"`
	Sg             string `arg:"--sg,required" help:"security group name or id"`
	SubnetIds      string `arg:"--subnets" help:"subnet-ids as space separated values"`
	Vpc            string `arg:"-v,--vpc" help:"vpc name or id"`
	Gigs           int    `arg:"-g,--gigs" help:"ebs gigabytes\n                        " default:"16"`
	Iops           int    `arg:"--iops" help:"gp3 iops\n                        " default:"3000"`
	Throughput     int    `arg:"--throughput" help:"gp3 throughput mb/s\n                        " default:"125"`
	Init           string `arg:"-i,--init" help:"cloud init bash script"`
	Tags           string `arg:"--tags" help:"space separated values like: key=value"`
	Profile        string `arg:"-p,--profile" help:"iam instance profile name"`
	SecondsTimeout int    `arg:"--seconds-timeout" default:"3600" help:"will $(sudo poweroff) after this many seconds.\n                         calls $(bash /etc/timeout.sh) and waits 60 seconds for it to exit before calling $(sudo poweroff).\n                         set to 0 to disable.\n                         $(sudo journalctl -f -u timeout.service) to follow logs.\n                        "`
	Wait           bool   `arg:"-w,--wait" default:"false" help:"wait for ssh"`
}

func (ec2NewArgs) Description() string {
	return "\ncreate ec2 instances\n"
}

func useSubnetsFromVpc(ctx context.Context, args *ec2NewArgs) {
	if args.Vpc != "" {
		zones, err := lib.EC2ZonesWithInstance(ctx, ec2types.InstanceType(args.Type))
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		if len(zones) == 0 {
			lib.Logger.Fatal("error: no zones contain instance type " + args.Type)
		}
		vpcID := args.Vpc
		if !strings.HasPrefix("vpc-", args.Vpc) {
			vpcID, err = lib.VpcID(ctx, args.Vpc)
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}
		}
		subnets, err := lib.VpcSubnets(ctx, vpcID)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		if args.SpotStrategy == "" {
			zone := zones[rand.Intn(len(zones))]
			for _, subnet := range subnets {
				if *subnet.AvailabilityZone == zone {
					args.SubnetIds = *subnet.SubnetId
					break
				}
			}
			if len(lib.SplitWhiteSpace(args.SubnetIds)) != 1 {
				lib.Logger.Fatalf("no subnet in zone %s for vpc %s", zone, vpcID)
			}
		} else {
			for _, subnet := range subnets {
				args.SubnetIds = " " + *subnet.SubnetId
			}
			if len(lib.SplitWhiteSpace(args.SubnetIds)) == 0 {
				lib.Logger.Fatalf("no subnets for vpc %s", vpcID)
			}
		}
	}
}

func ec2New() {
	var args ec2NewArgs
	p := arg.MustParse(&args)
	ctx, cancel := context.WithCancel(context.Background())
	lib.SignalHandler(cancel)
	if lib.Exists(args.Init) {
		data, err := os.ReadFile(args.Init)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		args.Init = string(data)
	}
	if args.Vpc == "" && len(lib.SplitWhiteSpace(args.SubnetIds)) == 0 {
		p.Fail("you must specify one of --vpc | --subnets")
	}
	if len(lib.SplitWhiteSpace(args.SubnetIds)) == 0 {
		useSubnetsFromVpc(ctx, &args)
	}
	if strings.HasPrefix(args.Ami, "ami-") {
		images, err := lib.EC2Client().DescribeImages(ctx, &ec2.DescribeImagesInput{
			Filters: []ec2types.Filter{{
				Name:   aws.String("image-id"),
				Values: []string{args.Ami},
			}},
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		if len(images.Images) != 1 {
			lib.Logger.Fatal("need exactly one image", lib.Pformat(images))
		}
		if args.UserName == "" {
			args.UserName = lib.EC2GetTag(images.Images[0].Tags, "user", "")
		}
	} else {
		arch := lib.EC2ArchAmd64
		if strings.Contains(strings.Split(args.Type, ".")[0][1:], "g") { // slice first char, since arm64 g is never first char
			arch = lib.EC2ArchArm64
		}
		ami, sshUser, err := lib.EC2AmiBase(ctx, args.Ami, arch)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		args.Ami = ami
		args.UserName = sshUser
	}
	if !strings.HasPrefix(args.Sg, "sg-") {
		sgID, err := lib.EC2SgID(ctx, args.Vpc, args.Sg)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		args.Sg = sgID
	}
	var instances []ec2types.Instance
	var tags []lib.EC2Tag
	for _, tag := range lib.SplitWhiteSpace(args.Tags) {
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
		InstanceType:   ec2types.InstanceType(args.Type),
		Name:           args.Name,
		Key:            args.Key,
		TempKey:        args.EphemeralKey,
		SgID:           args.Sg,
		SubnetIds:      lib.SplitWhiteSpace(args.SubnetIds),
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
		instances, err = lib.EC2RequestSpotFleet(ctx, ec2types.AllocationStrategy(args.SpotStrategy), fleetConfig)
	} else {
		instances, err = lib.EC2NewInstances(ctx, fleetConfig)
	}
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	var ids []string
	for _, instance := range instances {
		ids = append(ids, *instance.InstanceId)
	}
	if args.Wait {
		_, err := lib.EC2WaitSsh(ctx, &lib.EC2WaitSshInput{
			Selectors:      ids,
			MaxWaitSeconds: 300,
			User:           lib.EC2GetTag(instances[0].Tags, "user", ""),
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
	}
	for _, instance := range instances {
		fmt.Println(*instance.InstanceId)
	}
}
