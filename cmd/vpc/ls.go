package cliaws

import (
	"context"
	// "fmt"
	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
	// "os"
)

func init() {
	lib.Commands["vpc-ls"] = vpcLs
	lib.Args["vpc-ls"] = vpcLsArgs{}
}

type vpcLsArgs struct {
	// Selectors []string `arg:"positional" help:"instance-id | dns-name | private-dns-name | tag | vpc-id | subnet-id | security-group-id | ip-address | private-ip-address"`
	// State     string   `arg:"-s,--state" default:"" help:"running | pending | terminated | stopped"`
}

func (vpcLsArgs) Description() string {
	return "\nls vpcs\n"
}

func vpcLs() {
	var args vpcLsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	_ = ctx
}
