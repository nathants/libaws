package cliaws

import (
	"context"
	"fmt"
	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ec2-ls"] = ec2Ls
}

type lsArgs struct {
	Selectors []string `arg:"positional" help:"instance-ids | dns-names | private-dns-names | tags | vpc-id | subnet-id | security-group-id | ip-addresses | private-ip-addresses"`
	State     string   `arg:"-s,--state" default:"" help:"running | pending | terminated | stopped"`
}

func (lsArgs) Description() string {
	return "\nlist ec2 instances\n"
}

func ec2Ls() {
	var args lsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	instances, err := lib.EC2RetryListInstances(ctx, args.Selectors, args.State)
	if err != nil {
		lib.Logger.Fatal("error:", err)
	}
	for _, instance := range instances {
		fmt.Println(*instance.InstanceId, *instance.State.Name, *instance.InstanceType, lib.EC2Tags(instance.Tags))
	}
}
