package cliaws

import (
	"strings"
	"context"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ecr"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ecr-rm"] = ecrRm
}

type ecrRmArgs struct {
	Name string `arg:"positional,required"`
}

func (ecrRmArgs) Description() string {
	return "\nrm ecr\n"
}

func ecrRm() {
	var args ecrRmArgs
	arg.MustParse(&args)
	ctx := context.Background()
	parts := strings.Split(args.Name, ":")
	name := parts[0]
	tag := parts[1]
	_, err := lib.EcrClient().BatchDeleteImageWithContext(ctx, &ecr.BatchDeleteImageInput{
		RepositoryName: aws.String(name),
		ImageIds: []*ecr.ImageIdentifier{
			{
				ImageTag: aws.String(tag),
			},
		},
	})
	if err != nil {
	    lib.Logger.Fatal("error: ", err)
	}
}
