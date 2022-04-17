package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["vpc-rm"] = vpcRm
	lib.Args["vpc-rm"] = vpcRmArgs{}
}

type vpcRmArgs struct {
	Name string `arg:"positional,required"`
}

func (vpcRmArgs) Description() string {
	return `rm a vpc`
}

func vpcRm() {
	var args vpcRmArgs
	arg.MustParse(&args)
	ctx := context.Background()
	err := lib.VpcRm(ctx, args.Name)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
