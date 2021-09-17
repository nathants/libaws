package cliaws

import (
	"context"
	"fmt"
	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/nathants/cli-aws/lib"
	"time"
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
	images, err := lib.EC2Client().DescribeImagesWithContext(ctx, &ec2.DescribeImagesInput{
		Owners:  []*string{aws.String(account)},
		Filters: []*ec2.Filter{{Name: aws.String("image-id"), Values: []*string{aws.String(args.AmiID)}}},
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	if len(images.Images) != 1 {
		lib.Logger.Fatal("didn't find an image for id:", args.AmiID)
	}
	// find backing snapshot
	snaps, err := lib.EC2Client().DescribeSnapshotsWithContext(ctx, &ec2.DescribeSnapshotsInput{
		OwnerIds: []*string{aws.String(account)},
		Filters:  []*ec2.Filter{{Name: aws.String("description"), Values: []*string{aws.String(fmt.Sprintf("* %s *", args.AmiID))}}},
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, snapshot := range snaps.Snapshots {
		lib.Logger.Println("found backing snapshot:", *snapshot.SnapshotId)
	}
	// deregister image
	_, err = lib.EC2Client().DeregisterImageWithContext(ctx, &ec2.DeregisterImageInput{
		ImageId: aws.String(args.AmiID),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	lib.Logger.Println("deregistered:", args.AmiID)
	//
	for {
		images, err := lib.EC2Client().DescribeImagesWithContext(ctx, &ec2.DescribeImagesInput{
			Owners:  []*string{aws.String(account)},
			Filters: []*ec2.Filter{{Name: aws.String("image-id"), Values: []*string{aws.String(args.AmiID)}}},
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		if len(images.Images) == 0 || (len(images.Images) == 1 && *images.Images[0].State == ec2.ImageStateDeregistered) {
			break
		}
		lib.Logger.Println("wait for image to delete before deleting backing snapshot")
		time.Sleep(1 * time.Second)
	}
	// delete snapshot
	for _, snapshot := range snaps.Snapshots {
		_, err := lib.EC2Client().DeleteSnapshotWithContext(ctx, &ec2.DeleteSnapshotInput{
			SnapshotId: snapshot.SnapshotId,
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		lib.Logger.Println("deleted backing snapshot:", *snapshot.SnapshotId)
	}
}
