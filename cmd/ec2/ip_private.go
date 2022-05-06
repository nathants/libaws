package cliaws

import (
	"context"
	"fmt"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ec2-ip-private"] = ec2IpPrivate
	lib.Args["ec2-ip-private"] = ec2IpPrivateArgs{}
}

type ec2IpPrivateArgs struct {
	Selectors []string `arg:"positional" help:"instance-id | dns-name | private-dns-name | tag | vpc-id | subnet-id | security-group-id | ip-address | private-ip-address"`
}

func (ec2IpPrivateArgs) Description() string {
	return "\nlist ec2 private ipv4\n"
}

func ec2IpPrivate() {
	var args ec2IpPrivateArgs
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
		if instance.PrivateIpAddress != nil {
			fmt.Println(*instance.PrivateIpAddress)
		}
	}
}
