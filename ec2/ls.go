package ec2

import (
	"context"
	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ec2-ls"] = ec2Ls
}

type lsArgs struct {
}

func (lsArgs) Description() string {
	return "\nlist ec2 instances\n"
}

func ec2Ls() {
	var args lsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	_ = ctx
}
