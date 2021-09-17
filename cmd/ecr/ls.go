package cliaws

import (
	"fmt"
	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/service/ecr"
	"github.com/nathants/cli-aws/lib"
	"sort"
	"time"
)

func init() {
	lib.Commands["ecr-ls"] = ecrLs
	lib.Args["ecr-ls"] = ecrLsArgs{}
}

type ecrLsArgs struct {
}

func (ecrLsArgs) Description() string {
	return "\nlist ecr\n"
}

func ecrLs() {
	var args ecrLsArgs
	arg.MustParse(&args)
	repos, err := lib.EcrClient().DescribeRepositories(&ecr.DescribeRepositoriesInput{})
	if err != nil {
		panic(err)
	}
	for _, repo := range repos.Repositories {
		images, err := lib.EcrClient().DescribeImages(&ecr.DescribeImagesInput{
			RepositoryName: repo.RepositoryName,
		})
		if err != nil {
			panic(err)
		}
		sort.Slice(images.ImageDetails, func(a, b int) bool {
			return images.ImageDetails[a].ImagePushedAt.After(*images.ImageDetails[b].ImagePushedAt)
		})
		for _, image := range images.ImageDetails {
			for _, tag := range image.ImageTags {
				fmt.Println(*repo.RepositoryName+":"+*tag, *image.ImageDigest, image.ImagePushedAt.Format(time.RFC3339))
			}
		}
	}
}
