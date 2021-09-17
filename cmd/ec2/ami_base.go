package cliaws

import (
	"context"
	"fmt"
	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ec2-ami-base"] = ec2AmiBase
	lib.Args["ec2-ami-base"] = ec2AmiBaseArgs{}
}

type ec2AmiBaseArgs struct {
	Name string `arg:"positional,required" help:"arch | amzn | lambda | deeplearning | bionic | xenial | trusty | focal"`
}

func (ec2AmiBaseArgs) Description() string {
	return "\nget the latest ami-id for a given base ami name\n"
}

func ec2AmiBase() {
	var args ec2AmiBaseArgs
	arg.MustParse(&args)
	ctx := context.Background()
	amiID, _, err := lib.EC2Ami(ctx, args.Name)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(amiID)
}
