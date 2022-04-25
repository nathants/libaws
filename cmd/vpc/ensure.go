package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["vpc-ensure"] = vpcEnsure
	lib.Args["vpc-ensure"] = vpcEnsureArgs{}
}

type vpcEnsureArgs struct {
	Name string `arg:"positional,required"`
	XX   int    `arg:"-x,--xx" default:"0"`
}

func (vpcEnsureArgs) Description() string {
	return `setup a default-like vpc, with cidr 10.xx.0.0/16, a subnet for each zone like 10.xx.yy.0/20, and public ip on.`
}

func vpcEnsure() {
	var args vpcEnsureArgs
	arg.MustParse(&args)
	ctx := context.Background()
	vpc, err := lib.VpcEnsure(ctx, args.Name, args.XX)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(vpc)
}
