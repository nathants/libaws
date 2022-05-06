package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["vpc-ensure"] = vpcEnsure
	lib.Args["vpc-ensure"] = vpcEnsureArgs{}
}

type vpcEnsureArgs struct {
	Name    string `arg:"positional,required"`
	Preview bool   `arg:"-p,--preview"`
}

func (vpcEnsureArgs) Description() string {
	return "\nensure a default-like vpc, with cidr 10.xx.0.0/16, a subnet for each zone like 10.xx.yy.0/20, and public ip on.\n"
}

func vpcEnsure() {
	var args vpcEnsureArgs
	arg.MustParse(&args)
	ctx := context.Background()
	vpc, err := lib.VpcEnsure(ctx, "", args.Name, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(vpc)
}
