package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ec2-dns"] = ec2Dns
	lib.Args["ec2-dns"] = ec2DnsArgs{}
}

type ec2DnsArgs struct {
	Selectors []string `arg:"positional" help:"instance-id | dns-name | private-dns-name | tag | vpc-id | subnet-id | security-group-id | ip-address | private-ip-address"`
}

func (ec2DnsArgs) Description() string {
	return "\nlist ec2 dns\n"
}

func ec2Dns() {
	var args ec2DnsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	instances, err := lib.EC2ListInstances(ctx, args.Selectors, ec2.InstanceStateNameRunning)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, instance := range instances {
		if instance.PrivateDnsName != nil {
			fmt.Println(*instance.PublicDnsName)
		}
	}
}
