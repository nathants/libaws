package libaws

import (
	"context"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr"

	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ecr-rm-repo"] = ecrRmRepo
	lib.Args["ecr-rm-repo"] = ecrRmRepoArgs{}
}

type ecrRmRepoArgs struct {
	Name string `arg:"positional,required"`
}

func (ecrRmRepoArgs) Description() string {
	return "\ndelete ecr repository\n"
}

func ecrRmRepo() {
	var args ecrRmRepoArgs
	arg.MustParse(&args)
	ctx := context.Background()
	_, err := lib.EcrClient().DeleteRepository(ctx, &ecr.DeleteRepositoryInput{
		RepositoryName: aws.String(args.Name),
		Force:          true,
	})
	if err != nil {
		if !strings.Contains(err.Error(), "RepositoryNotFoundException") {
			lib.Logger.Fatal("error: ", err)
		}
	}
}
