package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["vpc-rm"] = vpcRm
	lib.Args["vpc-rm"] = vpcRmArgs{}
}

type vpcRmArgs struct {
	Name    string `arg:"positional,required"`
	Preview bool   `arg:"-p,--preview"`
}

func (vpcRmArgs) Description() string {
	return `rm a vpc`
}

func vpcRm() {
	var args vpcRmArgs
	arg.MustParse(&args)
	ctx := context.Background()
	err := lib.VpcRm(ctx, args.Name, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
