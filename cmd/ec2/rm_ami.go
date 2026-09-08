package libaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ec2-rm-ami"] = ec2RmAmi
	lib.Args["ec2-rm-ami"] = ec2RmAmiArgs{}
}

type ec2RmAmiArgs struct {
	AmiID   string `arg:"positional,required"`
	Preview bool   `arg:"-p,--preview"`
}

func (ec2RmAmiArgs) Description() string {
	return "\ndelete an owned ami and its backing snapshots, preserving snapshots used by other amis\n"
}

func ec2RmAmi() {
	var args ec2RmAmiArgs
	arg.MustParse(&args)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lib.SignalHandler(cancel)
	if err := lib.EC2DeleteAmi(ctx, args.AmiID, args.Preview); err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
