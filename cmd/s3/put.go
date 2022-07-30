package cliaws

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
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
	Path   string `arg:"positional"`
	Sha256 bool   `arg:"-s,--sha256" help:"add sha256 checksum"`
}

func (s3PutArgs) Description() string {
	return "\nput an object from stdin\n"
}

func s3Put() {
	var args s3PutArgs
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

	val, err := io.ReadAll(os.Stdin)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}

	input := &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(val),
	}

	if args.Sha256 {
		hash := sha256.Sum256(val)
		input.ChecksumAlgorithm = aws.String(s3.ChecksumAlgorithmSha256)
		input.ChecksumSHA256 = aws.String(base64.StdEncoding.EncodeToString(hash[:]))
	}

	_, err = s3Client.PutObjectWithContext(ctx, input)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
