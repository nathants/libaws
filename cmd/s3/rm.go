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
}

func (s3RmArgs) Description() string {
	return "\nrm s3 content \n"
}

func s3Rm() {
	var args s3RmArgs
	arg.MustParse(&args)
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
