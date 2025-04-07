package libaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
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
	return "\nensure a security-group\n" + `
libaws ec2-ensure-sg $vpc $sg tcp:22:0.0.0.0/0 tcp:443:0.0.0.0/0 ::sg-XXXX
`
}

func ec2EnsureSg() {
	var args ec2EnsureSgArgs
	arg.MustParse(&args)
	ctx := context.Background()
	input, err := lib.EC2EnsureSgInput("", args.VpcName, args.SgName, args.Rules)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	err = lib.EC2EnsureSg(ctx, input, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
