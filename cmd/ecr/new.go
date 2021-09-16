package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ecr"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ecr-new"] = ecrNew
lib.Args["ecr-new"] = ecrNewArgs{}
}

type ecrNewArgs struct {
	Name string `arg:"positional,required"`
}

func (ecrNewArgs) Description() string {
	return "\nnew ecr repo\n"
}

func ecrNew() {
	var args ecrNewArgs
	arg.MustParse(&args)
	ctx := context.Background()
	_, err := lib.EcrClient().CreateRepositoryWithContext(ctx, &ecr.CreateRepositoryInput{
		EncryptionConfiguration: &ecr.EncryptionConfiguration{
			EncryptionType: aws.String("AES256"),
		},
		RepositoryName: aws.String(args.Name),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
