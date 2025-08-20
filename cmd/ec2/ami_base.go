package libaws

import (
	"context"
	"fmt"
	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ec2-ami-base"] = ec2AmiBase
	lib.Args["ec2-ami-base"] = ec2AmiBaseArgs{}
}

type ec2AmiBaseArgs struct {
	Name string `arg:"positional,required" help:"ami-ID | amzn2 | amzn2023 | deeplearning | bionic | xenial | trusty | focal | jammy | bookworm | trixie | bullseye | buster | stretch | alpine-xx.yy.zz"`
	Arch string `arg:"-a,--arch" default:"x86_64" help:"arm64 | x86_64"`
}

func (ec2AmiBaseArgs) Description() string {
	return "\nget the latest ami-id for a given base ami name\n"
}

func ec2AmiBase() {
	var args ec2AmiBaseArgs
	arg.MustParse(&args)
	ctx := context.Background()
	amiID, _, err := lib.EC2AmiBase(ctx, args.Name, args.Arch)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(amiID)
}
