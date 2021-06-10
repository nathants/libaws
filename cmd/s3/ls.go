package cliaws

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["s3-ls"] = s3Ls
}

type s3LsArgs struct {
	Path      string `arg:"positional"`
	Recursive bool   `arg:"-r,--recursive"`
}

func (s3LsArgs) Description() string {
	return "\nlist s3 content\n"
}

func s3Ls() {
	var args s3LsArgs
	arg.MustParse(&args)
	ctx := context.Background()

	if args.Path == "" {
		out, err := lib.S3Client().ListBucketsWithContext(ctx, &s3.ListBucketsInput{})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		for _, bucket := range out.Buckets {
			fmt.Println(*bucket.Name)
		}
	} else {
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
		var delimiter *string
		if !args.Recursive {
			delimiter = aws.String("/")
		}

		var token *string
		for {
			out, err := s3Client.ListObjectsV2WithContext(ctx, &s3.ListObjectsV2Input{
				Bucket:            aws.String(bucket),
				Prefix:            aws.String(key),
				Delimiter:         delimiter,
				ContinuationToken: token,
			})
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}

			for _, pre := range out.CommonPrefixes {
				prefix := *pre.Prefix
				if key != "" {
					prefix = strings.SplitN(prefix, key, 2)[1]
				}
				fmt.Println(" PRE", prefix)
			}

			zone, _ := time.Now().Zone()
			loc, err := time.LoadLocation(zone)
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}

			for _, obj := range out.Contents {
				objKey := *obj.Key
				if key != "" && !args.Recursive {
					objKey = strings.SplitN(objKey, key, 2)[1]
				}
				fmt.Println(
					fmt.Sprint(obj.LastModified.In(loc))[:19],
					fmt.Sprintf("%10v", *obj.Size),
					objKey,
					*obj.StorageClass,
				)
			}

			if !*out.IsTruncated {
				break
			}

			token = out.ContinuationToken
		}
	}
}
