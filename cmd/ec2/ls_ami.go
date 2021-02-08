package cliaws

import (
	"context"
	"fmt"
	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/nathants/cli-aws/lib"
	"sort"
)

func init() {
	lib.Commands["ec2-ls-ami"] = ec2LsAmi
}

type lsAmiArgs struct {
}

func (lsAmiArgs) Description() string {
	return "\nlist amis\n"
}

func ec2LsAmi() {
	var args lsAmiArgs
	arg.MustParse(&args)
	ctx := context.Background()
	account, err := lib.Account(ctx)
	if err != nil {
		lib.Logger.Fatal("error:", err)
	}
	images, err := lib.EC2Client().DescribeImagesWithContext(ctx, &ec2.DescribeImagesInput{
		Owners: []*string{aws.String(account)},
	})
	if err != nil {
		lib.Logger.Fatal("error:", err)
	}
	sort.Slice(images.Images, func(i, j int) bool { return *images.Images[i].CreationDate > *images.Images[j].CreationDate })
	for _, image := range images.Images {
		fmt.Println(*image.ImageId, *image.CreationDate, lib.StringOr(image.Description, "-"), lib.EC2Tags(image.Tags))
	}
}
