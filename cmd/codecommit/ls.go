package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/service/codecommit"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["codecommit-ls"] = codeCommitLs
}

type codeCommitLsArgs struct {
}

func (codeCommitLsArgs) Description() string {
	return `
ls codecommit repositories
`
}

func codeCommitLs() {
	var args codeCommitLsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	var token *string
	var repos []*codecommit.RepositoryNameIdPair
	for {
		out, err := lib.CodeCommitClient().ListRepositoriesWithContext(ctx, &codecommit.ListRepositoriesInput{
			NextToken: token,
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		repos = append(repos, out.Repositories...)
		if out.NextToken == nil {
			break
		}
	}
	for _, repo := range repos {
		fmt.Println(*repo.RepositoryName)
	}
}
