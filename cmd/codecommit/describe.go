package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/codecommit"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["codecommit-describe"] = codeCommitDescribe
}

type codeCommitDescribeArgs struct {
	Name string `arg:"positional,required"`
}

func (codeCommitDescribeArgs) Description() string {
	return `
describe codecommit repository
`
}

func codeCommitDescribe() {
	var args codeCommitDescribeArgs
	arg.MustParse(&args)
	ctx := context.Background()
	getOut, err := lib.CodeCommitClient().GetRepositoryWithContext(ctx, &codecommit.GetRepositoryInput{
		RepositoryName: aws.String(args.Name),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(lib.Pformat(getOut.RepositoryMetadata))
}
