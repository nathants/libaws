package libaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ec2-rm-sg"] = ec2RmSg
	lib.Args["ec2-rm-sg"] = ec2RmSgArgs{}
}

type ec2RmSgArgs struct {
	VpcName string `arg:"positional,required"`
	SgName  string `arg:"positional,required"`
	Preview bool   `arg:"-p,--preview"`
}

func (ec2RmSgArgs) Description() string {
	return "\ndelete a security-group\n"
}

func ec2RmSg() {
	var args ec2RmSgArgs
	arg.MustParse(&args)
	ctx := context.Background()
	err := lib.EC2DeleteSg(ctx, args.VpcName, args.SgName, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
