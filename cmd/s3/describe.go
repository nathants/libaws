package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["s3-describe"] = s3Describe
	lib.Args["s3-describe"] = s3DescribeArgs{}
}

type s3DescribeArgs struct {
	Name string `arg:"positional,required"`
}

func (s3DescribeArgs) Description() string {
	return "\ndescribe a s3 bucket\n"
}

func s3Describe() {
	var args s3DescribeArgs
	arg.MustParse(&args)
	ctx := context.Background()
	bucket := args.Name
	descr, err := lib.S3GetBucketDescription(ctx, bucket)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(lib.Pformat(descr))
}
