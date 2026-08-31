package libaws

import (
	"context"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["s3-ls-versions"] = s3LsVersions
	lib.Args["s3-ls-versions"] = s3LsVersionsArgs{}
}

type s3LsVersionsArgs struct {
	Path      string `arg:"positional"`
	Recursive bool   `arg:"-r,--recursive"`
}

func (s3LsVersionsArgs) Description() string {
	return s3CommandDescription("list S3 content versions", false)
}

func s3LsVersions() {
	var args s3LsVersionsArgs
	arg.MustParse(&args)
	if err := lib.S3ListVersions(context.Background(), args.Path, args.Recursive, os.Stdout); err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
