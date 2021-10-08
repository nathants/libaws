package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
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
	instances, err := lib.EC2ListInstances(ctx, args.Selectors, "")
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, instance := range instances {
		fmt.Println(*instance.PrivateIpAddress)
	}
}
