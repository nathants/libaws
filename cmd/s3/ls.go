package cliaws

import (
	"context"
	"fmt"

	// "github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["s3-ls"] = s3Ls
}

type s3LsArgs struct {
}

func (s3LsArgs) Description() string {
	return "\nlist s3 buckets\n"
}

func s3Ls() {
	var args s3LsArgs
	arg.MustParse(&args)
	ctx := context.Background()

	out, err := lib.S3Client().ListBucketsWithContext(ctx, &s3.ListBucketsInput{})
	if err != nil {
	    lib.Logger.Fatal("error: ", err)
	}
	for _, bucket := range out.Buckets {
		fmt.Println(*bucket.Name)
	}
}
