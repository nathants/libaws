package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["vpc-id"] = vpcId
	lib.Args["vpc-id"] = vpcIdArgs{}
}

type vpcIdArgs struct {
	Name string `arg:"positional,required"`
}

func (vpcIdArgs) Description() string {
	return "\nget vpc id\n"
}

func vpcId() {
	var args vpcIdArgs
	arg.MustParse(&args)
	ctx := context.Background()
	id, err := lib.VpcID(ctx, args.Name)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(id)
}
