package cliaws

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["s3-rm"] = s3Rm
	lib.Args["s3-rm"] = s3RmArgs{}
}

type s3RmArgs struct {
	Path      string `arg:"positional,required"`
	Version   string `arg:"-v,--version"`
	Recursive bool   `arg:"-r,--recursive"`
	Preview   bool   `arg:"-p,--preview"`
}

func (s3RmArgs) Description() string {
	return "\nrm s3 content \n"
}

func s3Rm() {
	var args s3RmArgs
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

	var delimiter *string
	if !args.Recursive {
		delimiter = aws.String("/")
	}

	var marker *string
	for {
		out, err := s3Client.ListObjectsV2WithContext(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(bucket),
			Prefix:            aws.String(key),
			Delimiter:         delimiter,
			ContinuationToken: marker,
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}

		var objects []*s3.ObjectIdentifier

		for _, obj := range out.Contents {
			objects = append(objects, &s3.ObjectIdentifier{
				Key: obj.Key,
			})
		}

		if len(objects) != 0 {

			if !args.Preview {

				deleteOut, err := s3Client.DeleteObjectsWithContext(ctx, &s3.DeleteObjectsInput{
					Bucket: aws.String(bucket),
					Delete: &s3.Delete{Objects: objects},
				})
				if err != nil {
					lib.Logger.Fatal("error: ", err)
				}

				for _, err := range deleteOut.Errors {
					fmt.Println("error:", *err.Key, *err.Code, *err.Message)
				}
				if len(deleteOut.Errors) != 0 {
					os.Exit(1)
				}

			}

			for _, object := range objects {
				fmt.Println(lib.PreviewString(args.Preview)+"s3 deleted:", *object.Key)
			}

		}
		if out.NextContinuationToken == nil {
			break
		}

		marker = out.NextContinuationToken
	}

}
