package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ecr"
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
	_, err := lib.EcrClient().DeleteRepositoryWithContext(ctx, &ecr.DeleteRepositoryInput{
		RepositoryName: aws.String(args.Name),
		Force:          aws.Bool(true),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != "RepositoryNotFoundException" {
			lib.Logger.Fatal("error: ", err)
		}
	}
}
