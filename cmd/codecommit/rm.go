package cliaws

import (
	"context"
	"fmt"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/codecommit"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["codecommit-rm"] = codeCommitRm
	lib.Args["codecommit-rm"] = codeCommitRmArgs{}
}

type codeCommitRmArgs struct {
	Name string `arg:"positional,required"`
	Preview bool   `arg:"-p,--preview"`
}

func (codeCommitRmArgs) Description() string {
	return `
rm a codecommit repository
`
}

func codeCommitRm() {
	var args codeCommitRmArgs
	arg.MustParse(&args)
	ctx := context.Background()
	out, err := lib.CodeCommitClient().GetRepositoryWithContext(ctx, &codecommit.GetRepositoryInput{
		RepositoryName: aws.String(args.Name),
	})
	if err != nil {
		fmt.Println("repository not found")
		return
	}
	err = lib.PromptProceed("going to delete:\n" + lib.Pformat(out.RepositoryMetadata))
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	if args.Preview {
		os.Exit(0)
	}
	_, err = lib.CodeCommitClient().DeleteRepositoryWithContext(ctx, &codecommit.DeleteRepositoryInput{
		RepositoryName: aws.String(args.Name),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
