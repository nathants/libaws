package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ec2-ensure-sg"] = ec2EnsureSg
	lib.Args["ec2-ensure-sg"] = ec2EnsureSgArgs{}
}

type ec2EnsureSgArgs struct {
	VpcName string   `arg:"positional,required"`
	SgName  string   `arg:"positional,required"`
	Rules   []string `arg:"positional,required"`
}

func (ec2EnsureSgArgs) Description() string {
	return "\nensure a sg\n" + `
cli-aws ec2-ensure-sg $vpc $sg tcp:22:0.0.0.0/0 tcp:443:0.0.0.0/0
`
}

func ec2EnsureSg() {
	var args ec2EnsureSgArgs
	arg.MustParse(&args)
	ctx := context.Background()
	rules := []lib.EC2SgRule{}
	for _, r := range args.Rules {
		proto, port, cidr, err := lib.SplitTwice(r, ":")
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		rules = append(rules, lib.EC2SgRule{
			Proto: proto,
			Port:  lib.Atoi(port),
			Cidr:  cidr,
		})
	}
	err := lib.EC2EnsureSg(ctx, &lib.EC2EnsureSgInput{
		VpcName: args.VpcName,
		SgName:  args.SgName,
		Rules:   rules,
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
