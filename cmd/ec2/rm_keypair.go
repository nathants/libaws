package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ec2-rm-keypair"] = ec2RmKeypair
	lib.Args["ec2-rm-keypair"] = ec2RmKeypairArgs{}
}

type ec2RmKeypairArgs struct {
	Name string `arg:"positional,required"`
	Yes  bool   `arg:"-y,--yes" default:"false"`
}

func (ec2RmKeypairArgs) Description() string {
	return "\ndelete a keypair\n"
}

func ec2RmKeypair() {
	var args ec2RmKeypairArgs
	arg.MustParse(&args)
	ctx := context.Background()
	if !args.Yes {
		err := lib.PromptProceed("going to delete keypair: " + args.Name)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
	}
	_, err := lib.EC2Client().DeleteKeyPairWithContext(ctx, &ec2.DeleteKeyPairInput{
		KeyName: aws.String(args.Name),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
