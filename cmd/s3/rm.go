package libaws

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/nathants/libaws/lib"
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

	args.Path = strings.ReplaceAll(args.Path, "s3://", "")
	bucket, key, err := lib.SplitOnce(args.Path, "/")
	if err != nil {
		lib.Logger.Fatal("error: ", err)
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
		out, err := s3Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(bucket),
			Prefix:            aws.String(key),
			Delimiter:         delimiter,
			ContinuationToken: token,
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}

		var objects []s3types.ObjectIdentifier

		for _, obj := range out.Contents {
			objects = append(objects, s3types.ObjectIdentifier{
				Key: obj.Key,
			})
		}

		if len(objects) != 0 {

			if !args.Preview {

				deleteOut, err := s3Client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
					Bucket: aws.String(bucket),
					Delete: &s3types.Delete{Objects: objects},
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
		if !*out.IsTruncated {
			break
		}

		token = out.NextContinuationToken
	}

}
