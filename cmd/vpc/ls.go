package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["vpc-ls"] = vpcLs
	lib.Args["vpc-ls"] = vpcLsArgs{}
}

type vpcLsArgs struct {
}

func (vpcLsArgs) Description() string {
	return "\nls vpcs\n"
}

func vpcLs() {
	var args vpcLsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	vpcs, err := lib.VpcList(ctx)
	if err != nil {
	    lib.Logger.Fatal("error: ", err)
	}
	for _, vpc := range vpcs {
		fmt.Println(lib.EC2Name(vpc.Tags), *vpc.VpcId)
	}
}
