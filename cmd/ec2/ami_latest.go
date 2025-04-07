package libaws

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
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
	images, err := lib.EC2Client().DescribeImages(ctx, &ec2.DescribeImagesInput{
		Owners: []string{account},
		Filters: []ec2types.Filter{
			{
				Name: aws.String("description"),
				Values: []string{
					args.Name,
				},
			},
			{
				Name: aws.String("state"),
				Values: []string{
					"available",
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
