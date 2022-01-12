package cliaws

import (
	"context"
	"fmt"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ec2-new-ami"] = ec2NewAmi
	lib.Args["ec2-new-ami"] = ec2NewAmiArgs{}
}

type ec2NewAmiArgs struct {
	Name      string   `arg:"positional"`
	Selectors []string `arg:"positional" help:"instance-id | dns-name | private-dns-name | tag | vpc-id | subnet-id | security-group-id | ip-address | private-ip-address"`
	Wait      bool     `arg:"-w,--wait"`
}

func (ec2NewAmiArgs) Description() string {
	return "\nnew ami\n"
}

func ec2NewAmi() {
	var args ec2NewAmiArgs
	arg.MustParse(&args)
	ctx := context.Background()
	out, err := lib.EC2ListInstances(ctx, args.Selectors, "running")
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	if len(out) != 1 {
		lib.Logger.Fatal("error: not exactly 1 instance", lib.Pformat(out))
	}
	i := out[0]
	image, err := lib.EC2Client().CreateImageWithContext(ctx, &ec2.CreateImageInput{
		Name:        aws.String(fmt.Sprintf("%s__%d", args.Name, time.Now().Unix())),
		Description: aws.String(args.Name),
		InstanceId:  i.InstanceId,
		NoReboot:    aws.Bool(true),
		TagSpecifications: []*ec2.TagSpecification{{
			ResourceType: aws.String(ec2.ResourceTypeImage),
			Tags: []*ec2.Tag{{
				Key:   aws.String("user"),
				Value: aws.String(lib.EC2GetTag(i.Tags, "user", "")),
			}},
		}},
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	if args.Wait {
		for {
			status, err := lib.EC2Client().DescribeImagesWithContext(ctx, &ec2.DescribeImagesInput{
				ImageIds: []*string{image.ImageId},
			})
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}
			if *status.Images[0].State == ec2.ImageStateAvailable {
				break
			}
			lib.Logger.Println("wait for image", time.Now())
			time.Sleep(1 * time.Second)
		}
	}
	fmt.Println(*image.ImageId)
}
