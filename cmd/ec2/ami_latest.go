package cliaws

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ec2-ami-latest"] = ec2LatestAmi
	lib.Args["ec2-ami-latest"] = ec2LatestAmiArgs{}
}

type ec2LatestAmiArgs struct {
	Name string `arg:"positional,required"`
}

func (ec2LatestAmiArgs) Description() string {
	return "\nlatest ami\n"
}

func ec2LatestAmi() {
	var args ec2LatestAmiArgs
	arg.MustParse(&args)
	ctx := context.Background()
	account, err := lib.StsAccount(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	images, err := lib.EC2Client().DescribeImagesWithContext(ctx, &ec2.DescribeImagesInput{
		Owners: []*string{aws.String(account)},
		Filters: []*ec2.Filter{
			{
				Name: aws.String("description"),
				Values: []*string{
					&args.Name,
				},
			},
			{
				Name: aws.String("state"),
				Values: []*string{
					aws.String("available"),
				},
			},
		},
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	if len(images.Images) == 0 {
		os.Exit(1)
	}
	sort.Slice(images.Images, func(i, j int) bool { return *images.Images[i].CreationDate > *images.Images[j].CreationDate })
	image := images.Images[0]
	fmt.Println(*image.ImageId)
}
