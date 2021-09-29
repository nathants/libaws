package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ecr-ls"] = ecrLs
	lib.Args["ecr-ls"] = ecrLsArgs{}
}

type ecrLsArgs struct {
}

func (ecrLsArgs) Description() string {
	return "\nlist ecr containers\n"
}

func ecrLs() {
	var args ecrLsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	repos, err := lib.EcrDescribeRepos(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, repo := range repos {
		fmt.Println(*repo.RepositoryName)
	}
}
