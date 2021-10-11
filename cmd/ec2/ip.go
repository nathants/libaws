package cliaws

import (
	"context"
	"fmt"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ec2-ip"] = ec2Ip
	lib.Args["ec2-ip"] = ec2IpArgs{}
}

type ec2IpArgs struct {
	Selectors []string `arg:"positional" help:"instance-id | dns-name | private-dns-name | tag | vpc-id | subnet-id | security-group-id | ip-address | private-ip-address"`
}

func (ec2IpArgs) Description() string {
	return "\nlist ec2 ipv4\n"
}

func ec2Ip() {
	var args ec2IpArgs
	arg.MustParse(&args)
	ctx := context.Background()
	instances, err := lib.EC2ListInstances(ctx, args.Selectors, ec2.InstanceStateNameRunning)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	if len(instances) == 0 {
		os.Exit(1)
	}
	for _, instance := range instances {
		if instance.PublicIpAddress != nil {
			fmt.Println(*instance.PublicIpAddress)
		}
	}
}
