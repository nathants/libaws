package cliaws

import (
	"time"
	"fmt"
	"sort"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ecr"
	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ecr-ls-tags"] = ecrLsTags
	lib.Args["ecr-ls-tags"] = ecrLsTagsArgs{}
}

type ecrLsTagsArgs struct {
	Image string `arg:"positional,required" help:"image name"`
}

func (ecrLsTagsArgs) Description() string {
	return "\nlist ecr tags for an image\n"
}

func ecrLsTags() {
	var args ecrLsTagsArgs
	arg.MustParse(&args)
	images, err := lib.EcrClient().DescribeImages(&ecr.DescribeImagesInput{
		RepositoryName: aws.String(args.Image),
	})
	if err != nil {
	    lib.Logger.Fatal("error: ", err)
	}
	sort.Slice(images.ImageDetails, func(a, b int) bool {
		return images.ImageDetails[a].ImagePushedAt.After(*images.ImageDetails[b].ImagePushedAt)
	})
	for _, image := range images.ImageDetails {
		for _, tag := range image.ImageTags {
			fmt.Println(args.Image+":"+*tag, *image.ImageDigest, image.ImagePushedAt.Format(time.RFC3339))
		}
	}
}
