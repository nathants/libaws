package libaws

import (
	"context"
	"fmt"
	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/nathants/libaws/lib"
	"sort"
)

func init() {
	lib.Commands["ec2-ls-amis"] = ec2LsAmis
	lib.Args["ec2-ls-amis"] = ec2LsAmisArgs{}
}

type ec2LsAmisArgs struct {
}

func (ec2LsAmisArgs) Description() string {
	return "\nlist amis\n"
}

func ec2LsAmis() {
	var args ec2LsAmisArgs
	arg.MustParse(&args)
	ctx := context.Background()
	account, err := lib.StsAccount(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	images, err := lib.EC2Client().DescribeImages(ctx, &ec2.DescribeImagesInput{
		Owners: []string{account},
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	sort.Slice(images.Images, func(i, j int) bool { return *images.Images[i].CreationDate > *images.Images[j].CreationDate })
	for _, image := range images.Images {
		fmt.Println(
			*image.ImageId,
			*image.CreationDate,
			lib.StringOr(image.Description, "-"),
			lib.EC2Tags(image.Tags),
			string(image.State))
	}
}
