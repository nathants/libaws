package cliaws

import (
	"context"
	"os"

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
	lib.Logger.Println("going to delete keypair: " + args.Name)
	if args.Preview {
		os.Exit(0)
	}
	_, err := lib.EC2Client().DeleteKeyPairWithContext(ctx, &ec2.DeleteKeyPairInput{
		KeyName: aws.String(args.Name),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
