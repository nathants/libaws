package cliaws

import (
	"context"
	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ec2-rm-snapshot"] = ec2RmSnapshot
}

type rmSnapshotArgs struct {
	SnapshotID string `arg:"positional"`
}

func (rmSnapshotArgs) Description() string {
	return "\ndelete an snapshot\n"
}

func ec2RmSnapshot() {
	var args rmSnapshotArgs
	arg.MustParse(&args)
	ctx := context.Background()
	_, err := lib.EC2Client().DeleteSnapshotWithContext(ctx, &ec2.DeleteSnapshotInput{
		SnapshotId: aws.String(args.SnapshotID),
	})
	if err != nil {
		lib.Logger.Fatal("error:", err)
	}
	lib.Logger.Println("deleted snapshot:", args.SnapshotID)
}
