package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ec2-ami-user"] = ec2AmiUser
	lib.Args["ec2-ami-user"] = ec2AmiUserArgs{}
}

type ec2AmiUserArgs struct {
	AmiID string `arg:"positional,required"`
}

func (ec2AmiUserArgs) Description() string {
	return "\nget the latest ami-id for a given user ami name\n"
}

func ec2AmiUser() {
	var args ec2AmiUserArgs
	arg.MustParse(&args)
	ctx := context.Background()
	amiID, err := lib.EC2AmiUser(ctx, args.AmiID)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(amiID)
}
