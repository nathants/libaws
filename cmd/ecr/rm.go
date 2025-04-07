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
	lib.Commands["ecr-rm"] = ecrRm
	lib.Args["ecr-rm"] = ecrRmArgs{}
}

type ecrRmArgs struct {
	Name string `arg:"positional,required"`
	// Preview bool   `arg:"-p,--preview"`
}

func (ecrRmArgs) Description() string {
	return "\nrm ecr container\n"
}

func ecrRm() {
	var args ecrRmArgs
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
