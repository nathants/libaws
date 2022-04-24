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
	Rules   []string `arg:"positional"`
	Preview bool     `arg:"-p,--preview"`
}

func (ec2EnsureSgArgs) Description() string {
	return "\nensure a sg\n" + `
cli-aws ec2-ensure-sg $vpc $sg tcp:22:0.0.0.0/0 tcp:443:0.0.0.0/0 ::sg-XXXX
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
		rule := lib.EC2SgRule{
			Proto: proto,
			Cidr:  cidr,
		}
		if port != "" {
			rule.Port = lib.Atoi(port)
		}
		rules = append(rules, rule)
	}
	err := lib.EC2EnsureSg(ctx, &lib.EC2EnsureSgInput{
		VpcName: args.VpcName,
		SgName:  args.SgName,
		Rules:   rules,
		Preview: args.Preview,
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
