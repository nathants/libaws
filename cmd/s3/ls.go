package libaws

import (
	"context"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["s3-ls"] = s3Ls
	lib.Args["s3-ls"] = s3LsArgs{}
}

type s3LsArgs struct {
	Path       string `arg:"positional"`
	Quiet      bool   `arg:"-q,--quiet" help:"print key only"`
	Recursive  bool   `arg:"-r,--recursive" help:"list all keys with prefix path"`
	StartAfter bool   `arg:"-s,--start-after" help:"list all keys that lexically appear after path"`
	R2         bool   `arg:"--r2" help:"use Cloudflare R2 credentials and endpoint"`
}

func (s3LsArgs) Description() string {
	return s3CommandDescription("list S3 content", true)
}

func s3Ls() {
	var args s3LsArgs
	arg.MustParse(&args)
	configureS3Provider(args.R2)
	if err := lib.S3List(context.Background(), &lib.S3ListInput{
		Path: args.Path, Quiet: args.Quiet, Recursive: args.Recursive, StartAfter: args.StartAfter,
	}, os.Stdout); err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
