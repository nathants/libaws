package libaws

import (
	"fmt"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["s3-presign-get"] = s3PresignGet
	lib.Args["s3-presign-get"] = s3PresignGetArgs{}
}

type s3PresignGetArgs struct {
	Path              string `arg:"positional"`
	ExpirationMinutes int    `arg:"-e,--expiration-minutes" default:"20"`
	Range             string `arg:"-r,--range" help:"bytes=0-10"`
}

func (s3PresignGetArgs) Description() string {
	return s3CommandDescription("presign an S3 GET URL", false)
}

func s3PresignGet() {
	var args s3PresignGetArgs
	arg.MustParse(&args)
	bucket, key, err := lib.S3SplitPath(args.Path)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(lib.S3PresignGet(bucket, key, args.Range, time.Duration(args.ExpirationMinutes)*time.Minute))
}
