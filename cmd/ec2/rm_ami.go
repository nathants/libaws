package libaws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"time"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ec2-rm-ami"] = ec2RmAmi
	lib.Args["ec2-rm-ami"] = ec2RmAmiArgs{}
}

type ec2RmAmiArgs struct {
	AmiID string `arg:"positional"`
}

func (ec2RmAmiArgs) Description() string {
	return "\ndelete an ami\n"
}

func ec2RmAmi() {
	var args ec2RmAmiArgs
	arg.MustParse(&args)
	ctx := context.Background()
	account, err := lib.StsAccount(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	// find image
	images, err := lib.EC2Client().DescribeImages(ctx, &ec2.DescribeImagesInput{
		Owners:  []string{account},
		Filters: []ec2types.Filter{{Name: aws.String("image-id"), Values: []string{args.AmiID}}},
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	if len(images.Images) != 1 {
		lib.Logger.Fatal("didn't find image for id:", args.AmiID)
	}
	// find backing snapshot
	snaps, err := lib.EC2Client().DescribeSnapshots(ctx, &ec2.DescribeSnapshotsInput{
		OwnerIds: []string{account},
		Filters:  []ec2types.Filter{{Name: aws.String("description"), Values: []string{fmt.Sprintf("* %s *", args.AmiID)}}},
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, snapshot := range snaps.Snapshots {
		lib.Logger.Println("found backing snapshot:", *snapshot.SnapshotId)
	}
	// deregister image
	_, err = lib.EC2Client().DeregisterImage(ctx, &ec2.DeregisterImageInput{
		ImageId: aws.String(args.AmiID),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	lib.Logger.Println("deregistered:", args.AmiID)
	//
	for {
		images, err := lib.EC2Client().DescribeImages(ctx, &ec2.DescribeImagesInput{
			Owners:  []string{account},
			Filters: []ec2types.Filter{{Name: aws.String("image-id"), Values: []string{args.AmiID}}},
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		if len(images.Images) == 0 || (len(images.Images) == 1 && images.Images[0].State == ec2types.ImageStateDeregistered) {
			break
		}
		lib.Logger.Println("wait for image to delete before deleting backing snapshot")
		time.Sleep(1 * time.Second)
	}
	// delete snapshot
	for _, snapshot := range snaps.Snapshots {
		_, err := lib.EC2Client().DeleteSnapshot(ctx, &ec2.DeleteSnapshotInput{
			SnapshotId: snapshot.SnapshotId,
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		lib.Logger.Println("deleted backing snapshot:", *snapshot.SnapshotId)
	}
}
