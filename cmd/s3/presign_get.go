package cliaws

import (
	"fmt"
	"strings"
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
}

func (s3PresignGetArgs) Description() string {
	return "\npresign a  s3 get url\n"
}

func s3PresignGet() {
	var args s3PresignGetArgs
	arg.MustParse(&args)
	args.Path = strings.ReplaceAll(args.Path, "s3://", "")
	bucket, key, err := lib.SplitOnce(args.Path, "/")
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(lib.S3PresignGet(bucket, key, "", time.Duration(args.ExpirationMinutes)*time.Minute))
}
