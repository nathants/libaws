package libaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ec2-rm-keypair"] = ec2RmKeypair
	lib.Args["ec2-rm-keypair"] = ec2RmKeypairArgs{}
}

type ec2RmKeypairArgs struct {
	Name    string `arg:"positional,required"`
	Preview bool   `arg:"-p,--preview"`
}

func (ec2RmKeypairArgs) Description() string {
	return "\ndelete a keypair\n"
}

func ec2RmKeypair() {
	var args ec2RmKeypairArgs
	arg.MustParse(&args)
	ctx := context.Background()
	err := lib.EC2DeleteKeypair(ctx, args.Name, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
