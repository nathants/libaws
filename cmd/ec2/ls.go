package cliaws

import (
	"context"
	"fmt"
	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
	"os"
)

func init() {
	lib.Commands["ec2-ls"] = ec2Ls
}

type ec2LsArgs struct {
	Selectors []string `arg:"positional" help:"instance-ids | dns-names | private-dns-names | tags | vpc-id | subnet-id | security-group-id | ip-addresses | private-ip-addresses"`
	State     string   `arg:"-s,--state" default:"" help:"running | pending | terminated | stopped"`
}

func (ec2LsArgs) Description() string {
	return "\nlist ec2 instances\n"
}

func ec2Ls() {
	var args ec2LsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	instances, err := lib.EC2RetryListInstances(ctx, args.Selectors, args.State)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Fprintln(os.Stderr, "name", "type", "state", "id", "image", "kind", "security-group", "tags")
	for _, instance := range instances {
		fmt.Println(
			lib.EC2Name(instance.Tags),
			*instance.InstanceType,
			lib.EC2State(instance),
			*instance.InstanceId,
			*instance.ImageId,
			lib.EC2Kind(instance),
			lib.EC2SecurityGroups(instance.SecurityGroups),
			lib.EC2Tags(instance.Tags),
		)
	}
}
