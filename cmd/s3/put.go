package cliaws

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["s3-put"] = s3Put
	lib.Args["s3-put"] = s3PutArgs{}
}

type s3PutArgs struct {
	Path string `arg:"positional"`
}

func (s3PutArgs) Description() string {
	return "\nput an object from stdin\n"
}

func s3Put() {
	var args s3PutArgs
	arg.MustParse(&args)
	ctx := context.Background()

	path := lib.Last(strings.Split(args.Path, "s3://"))
	parts := strings.Split(path, "/")
	bucket := parts[0]
	var key string
	if len(parts) > 1 {
		key = strings.Join(parts[1:], "/")
	}

	s3Client, err := lib.S3ClientBucketRegion(bucket)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}

	val, err := io.ReadAll(os.Stdin)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}

	_, err = s3Client.PutObjectWithContext(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(val),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
