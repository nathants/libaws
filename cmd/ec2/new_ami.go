package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ec2-new-ami"] = ec2NewAmi
	lib.Args["ec2-new-ami"] = ec2NewAmiArgs{}
}

type ec2NewAmiArgs struct {
	Selectors []string `arg:"positional" help:"instance-id | dns-name | private-dns-name | tag | vpc-id | subnet-id | security-group-id | ip-address | private-ip-address"`
	Wait      bool     `arg:"-w,--wait" default:"false"`
}

func (ec2NewAmiArgs) Description() string {
	return "\nnew ami\n"
}

func ec2NewAmi() {
	var args ec2NewAmiArgs
	arg.MustParse(&args)
	ctx := context.Background()
	amiID, err := lib.EC2NewAmi(ctx, &lib.EC2NewAmiInput{
		Selectors: args.Selectors,
		Wait:      args.Wait,
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(amiID)
}
