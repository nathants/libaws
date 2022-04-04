package cliaws

import (
	"fmt"
	"sort"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ecr"
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
	var imageDetails []*ecr.ImageDetail
	var token *string
	for {
		out, err := lib.EcrClient().DescribeImages(&ecr.DescribeImagesInput{
			RepositoryName: aws.String(args.Image),
			NextToken:      token,
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		imageDetails = append(imageDetails, out.ImageDetails...)
		if out.NextToken == nil {
			break
		}
		token = out.NextToken
	}
	sort.Slice(imageDetails, func(a, b int) bool {
		return imageDetails[a].ImagePushedAt.After(*imageDetails[b].ImagePushedAt)
	})
	for _, image := range imageDetails {
		for _, tag := range image.ImageTags {
			fmt.Println(args.Image+":"+*tag, *image.ImageDigest, image.ImagePushedAt.Format(time.RFC3339))
		}
	}
}
