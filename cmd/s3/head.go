package cliaws

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["s3-head"] = s3Head
	lib.Args["s3-head"] = s3HeadArgs{}
}

type s3HeadArgs struct {
	Path string `arg:"positional"`
}

func (s3HeadArgs) Description() string {
	return "\nhead an object\n"
}

func s3Head() {
	var args s3HeadArgs
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

	out, err := s3Client.HeadObjectWithContext(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}

	data, err := json.Marshal(out)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	var outMap map[string]interface{}
	err = json.Unmarshal(data, &outMap)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}

	val := map[string]interface{}{}
	for k, v := range outMap {
		if v != nil {
			val[k] = v
		}
	}
	fmt.Println(lib.Pformat(val))
}
