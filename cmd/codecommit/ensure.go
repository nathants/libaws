package libaws

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/codecommit"
	cctypes "github.com/aws/aws-sdk-go-v2/service/codecommit/types"
	"github.com/nathants/libaws/lib"
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
	createOut, err := lib.CodeCommitClient().CreateRepository(ctx, &codecommit.CreateRepositoryInput{
		RepositoryName:        aws.String(args.Name),
		RepositoryDescription: aws.String(args.Descr),
	})
	if err == nil {
		fmt.Fprintln(os.Stderr, "created:", args.Name)
		fmt.Println(lib.Pformat(createOut.RepositoryMetadata))
		return
	}
	var rne *cctypes.RepositoryNameExistsException
	if errors.As(err, &rne) {
	} else {
		lib.Logger.Fatal("error: ", err)
	}
}
