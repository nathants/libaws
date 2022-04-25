package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ec2-ls-sgs"] = ec2LsSgs
	lib.Args["ec2-ls-sgs"] = ec2LsSgsArgs{}
}

type ec2LsSgsArgs struct {
}

func (ec2LsSgsArgs) Description() string {
	return "\nls sgs\n"
}

func ec2LsSgs() {
	var args ec2LsSgsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	out, err := lib.EC2ListSg(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, sg := range out {
		fmt.Println(*sg.GroupName, *sg.GroupId, *sg.VpcId)
	}
}
