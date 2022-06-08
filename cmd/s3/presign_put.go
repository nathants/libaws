package cliaws

import (
	"fmt"
	"strings"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["s3-presign-put"] = s3PresignPut
	lib.Args["s3-presign-put"] = s3PresignPutArgs{}
}

type s3PresignPutArgs struct {
	Path              string `arg:"positional"`
	ExpirationMinutes int    `arg:"-e,--expiration-minutes" default:"20"`
}

func (s3PresignPutArgs) Description() string {
	return "\npresign a  s3 put url\n"
}

func s3PresignPut() {
	var args s3PresignPutArgs
	arg.MustParse(&args)
	path := lib.Last(strings.Split(args.Path, "s3://"))
	parts := strings.Split(path, "/")
	bucket := parts[0]
	var key string
	if len(parts) > 1 {
		key = strings.Join(parts[1:], "/")
	}
	fmt.Println(lib.S3PresignPut(bucket, key, time.Duration(args.ExpirationMinutes)*time.Minute))
}
