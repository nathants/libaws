package cliaws

import (
	"context"
	"fmt"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/codecommit"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["codecommit-ensure"] = codeCommitEnsure
	lib.Args["codecommit-ensure"] = codeCommitEnsureArgs{}
}

type codeCommitEnsureArgs struct {
	Name  string `arg:"positional,required"`
	Descr string `arg:"-d,--description"`
}

func (codeCommitEnsureArgs) Description() string {
	return `
ensure a codecommit repository
`
}

func codeCommitEnsure() {
	var args codeCommitEnsureArgs
	arg.MustParse(&args)
	ctx := context.Background()
	createOut, err := lib.CodeCommitClient().CreateRepositoryWithContext(ctx, &codecommit.CreateRepositoryInput{
		RepositoryName:        aws.String(args.Name),
		RepositoryDescription: aws.String(args.Descr),
	})
	if err == nil {
		fmt.Fprintln(os.Stderr, "created:", args.Name)
		fmt.Println(lib.Pformat(createOut.RepositoryMetadata))
		return
	}
	aerr, ok := err.(awserr.Error)
	if !ok || aerr.Code() != codecommit.ErrCodeRepositoryNameExistsException {
		lib.Logger.Fatal("error: ", err)
	}
}
