package libaws

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/nathants/libaws/lib"
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

	args.Path = strings.ReplaceAll(args.Path, "s3://", "")
	bucket, key, err := lib.SplitOnce(args.Path, "/")
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}

	s3Client, err := lib.S3ClientBucketRegion(bucket)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	out, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
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
