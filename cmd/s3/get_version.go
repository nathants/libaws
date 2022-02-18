package cliaws

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["s3-get-version"] = s3Getversion
	lib.Args["s3-get-version"] = s3GetVersionArgs{}
}

type s3GetVersionArgs struct {
	Path    string `arg:"positional"`
	Version string `arg:"-v,--version,required"`
}

func (s3GetVersionArgs) Description() string {
	return "\nget an object version and write it to stdout\n"
}

func s3Getversion() {
	var args s3GetVersionArgs
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

	out, err := s3Client.GetObjectWithContext(ctx, &s3.GetObjectInput{
		Bucket:    aws.String(bucket),
		Key:       aws.String(key),
		VersionId: aws.String(args.Version),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	defer func() { _ = out.Body.Close() }()

	_, err = io.Copy(os.Stdout, out.Body)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
