package libaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ec2-ensure-keypair"] = ec2EnsureKeypair
	lib.Args["ec2-ensure-keypair"] = ec2EnsureKeypairArgs{}
}

type ec2EnsureKeypairArgs struct {
	Name          string `arg:"positional,required"`
	PubkeyContent string `arg:"positional,required"`
	Preview       bool   `arg:"-p,--preview"`
}

func (ec2EnsureKeypairArgs) Description() string {
	return "\nensure a keypair\n"
}

func ec2EnsureKeypair() {
	var args ec2EnsureKeypairArgs
	arg.MustParse(&args)
	ctx := context.Background()
	err := lib.EC2EnsureKeypair(ctx, "", args.Name, args.PubkeyContent, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
