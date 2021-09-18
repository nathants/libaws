package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ecr-ls-images"] = ecrLsImages
	lib.Args["ecr-ls-images"] = ecrLsImagesArgs{}
}

type ecrLsImagesArgs struct {
}

func (ecrLsImagesArgs) Description() string {
	return "\nlist ecr images\n"
}

func ecrLsImages() {
	var args ecrLsImagesArgs
	arg.MustParse(&args)
	ctx := context.Background()
	repos, err := lib.EcrDescribeRepos(ctx)
	if err != nil {
	    lib.Logger.Fatal("error: ", err)
	}
	for _, repo := range repos {
		fmt.Println(*repo.RepositoryName)
	}
}
