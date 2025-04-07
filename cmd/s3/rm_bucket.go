package libaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["s3-rm-bucket"] = s3RmBucket
	lib.Args["s3-rm-bucket"] = s3RmBucketArgs{}
}

type s3RmBucketArgs struct {
	Bucket  string `arg:"positional,required"`
	Preview bool   `arg:"-p,--preview"`
}

func (s3RmBucketArgs) Description() string {
	return "\nrm s3 bucket\n"
}

func s3RmBucket() {
	var args s3RmBucketArgs
	arg.MustParse(&args)
	ctx := context.Background()
	err := lib.S3DeleteBucket(ctx, args.Bucket, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
