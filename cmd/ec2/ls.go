package cliaws

import (
	"context"
	"fmt"
	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
	"os"
)

func init() {
	lib.Commands["ec2-ls"] = ec2Ls
	lib.Args["ec2-ls"] = ec2LsArgs{}
}

type ec2LsArgs struct {
	Selectors []string `arg:"positional" help:"instance-id | dns-name | private-dns-name | tag | vpc-id | subnet-id | security-group-id | ip-address | private-ip-address"`
	State     string   `arg:"-s,--state" default:"" help:"running | pending | terminated | stopped"`
}

func (ec2LsArgs) Description() string {
	return "\nlist ec2 instances\n"
}

func ec2Ls() {
	var args ec2LsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	instances, err := lib.EC2ListInstances(ctx, args.Selectors, args.State)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Fprintln(os.Stderr, "name", "type", "state", "id", "image", "kind", "security-group", "tags")
	count := 0
	for _, instance := range instances {
		count++
		subnet := "-"
		if instance.SubnetId != nil {
			subnet = *instance.SubnetId
		}
		fmt.Println(
			lib.EC2NameColored(instance),
			*instance.InstanceType,
			*instance.State.Name,
			*instance.InstanceId,
			*instance.ImageId,
			lib.EC2Kind(instance),
			subnet,
			lib.EC2SecurityGroups(instance.SecurityGroups),
			lib.EC2Tags(instance.Tags),
		)
	}
	if count == 0 {
		os.Exit(1)
	}
}
