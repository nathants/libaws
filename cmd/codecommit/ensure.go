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
	if !ok {
		lib.Logger.Fatal("error: ", err)
	}
	switch aerr.Code() {
	case codecommit.ErrCodeRepositoryNameExistsException:
		fmt.Fprintln(os.Stderr, "exists:", args.Name)
		getOut, err := lib.CodeCommitClient().GetRepositoryWithContext(ctx, &codecommit.GetRepositoryInput{
			RepositoryName: aws.String(args.Name),
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		fmt.Println(lib.Pformat(getOut.RepositoryMetadata))
	default:
		lib.Logger.Fatal("error: ", err)
	}
}
