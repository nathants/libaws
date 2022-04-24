package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ec2-id-sg"] = ec2IdSg
	lib.Args["ec2-id-sg"] = ec2IdSgArgs{}
}

type ec2IdSgArgs struct {
	VpcName string   `arg:"positional,required"`
	SgName string   `arg:"positional,required"`
}

func (ec2IdSgArgs) Description() string {
	return "\nsg id\n"
}

func ec2IdSg() {
	var args ec2IdSgArgs
	arg.MustParse(&args)
	ctx := context.Background()
	id, err := lib.EC2SgID(ctx, args.VpcName, args.SgName)
	if err != nil {
	    lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(id)
}
