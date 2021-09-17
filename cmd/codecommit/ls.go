package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["codecommit-ls"] = codeCommitLs
	lib.Args["codecommit-ls"] = codeCommitLsArgs{}
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
	repos, err := lib.CodeCommitListRepos(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, repo := range repos {
		fmt.Println(*repo.RepositoryName)
	}
}
