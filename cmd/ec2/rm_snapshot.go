package libaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ec2-rm-snapshot"] = ec2RmSnapshot
	lib.Args["ec2-rm-snapshot"] = ec2RmSnapshotArgs{}
}

type ec2RmSnapshotArgs struct {
	SnapshotID string `arg:"positional"`
}

func (ec2RmSnapshotArgs) Description() string {
	return "\ndelete an snapshot\n"
}

func ec2RmSnapshot() {
	var args ec2RmSnapshotArgs
	arg.MustParse(&args)
	ctx := context.Background()
	_, err := lib.EC2Client().DeleteSnapshot(ctx, &ec2.DeleteSnapshotInput{
		SnapshotId: aws.String(args.SnapshotID),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	lib.Logger.Println("deleted snapshot:", args.SnapshotID)
}
