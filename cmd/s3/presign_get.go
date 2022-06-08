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
	path := lib.Last(strings.Split(args.Path, "s3://"))
	parts := strings.Split(path, "/")
	bucket := parts[0]
	var key string
	if len(parts) > 1 {
		key = strings.Join(parts[1:], "/")
	}
	fmt.Println(lib.S3PresignGet(bucket, key, "", time.Duration(args.ExpirationMinutes)*time.Minute))
}
