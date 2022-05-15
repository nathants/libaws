package cliaws

import (
	"context"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ecr"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ecr-rm-tag"] = ecrRmTag
	lib.Args["ecr-rm-tag"] = ecrRmTagArgs{}
}

type ecrRmTagArgs struct {
	Name string `arg:"positional,required"`
}

func (ecrRmTagArgs) Description() string {
	return "\nrm ecr tag\n"
}

func ecrRmTag() {
	var args ecrRmTagArgs
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
