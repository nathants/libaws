package libaws

import (
	"context"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["s3-rm"] = s3Rm
	lib.Args["s3-rm"] = s3RmArgs{}
}

type s3RmArgs struct {
	Path      string `arg:"positional,required"`
	Recursive bool   `arg:"-r,--recursive"`
	Preview   bool   `arg:"-p,--preview"`
	R2        bool   `arg:"--r2" help:"use Cloudflare R2 credentials and endpoint"`
}

func (s3RmArgs) Description() string {
	return s3CommandDescription("remove S3 content", true)
}

func s3Rm() {
	var args s3RmArgs
	arg.MustParse(&args)
	configureS3Provider(args.R2)
	ctx := context.Background()

	args.Path = strings.ReplaceAll(args.Path, "s3://", "")
	bucket, key, err := lib.SplitOnce(args.Path, "/")
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}

	deleteInput := &lib.S3DeleteInput{
		Bucket:    bucket,
		Prefix:    key,
		Recursive: args.Recursive,
		Preview:   args.Preview,
	}

	err = lib.S3Delete(ctx, deleteInput)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
