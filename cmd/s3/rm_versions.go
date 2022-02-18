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
	lib.Commands["s3-rm-versions"] = s3RmVersions
	lib.Args["s3-rm-versions"] = s3RmVersionsArgs{}
}

type s3RmVersionsArgs struct {
	Path      string `arg:"positional,required"`
	Version   string `arg:"-v,--version"`
	Recursive bool   `arg:"-r,--recursive"`
	Preview   bool   `arg:"-p,--preview"`
}

func (s3RmVersionsArgs) Description() string {
	return "\nrm s3 content versions\n"
}

func s3RmVersions() {
	var args s3RmVersionsArgs
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

	if args.Version != "" {

		if !args.Preview {

			out, err := s3Client.DeleteObjectsWithContext(ctx, &s3.DeleteObjectsInput{
				Bucket: aws.String(bucket),
				Delete: &s3.Delete{
					Objects: []*s3.ObjectIdentifier{{
						Key:       aws.String(key),
						VersionId: aws.String(args.Version),
					}},
				},
			})
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}

			for _, err := range out.Errors {
				version := *err.VersionId
				if version == "" {
					version = "-"
				}
				fmt.Println("error:", *err.Key, version, *err.Code, *err.Message)
			}

			if len(out.Errors) != 0 {
				os.Exit(1)
			}

		}

		lib.Logger.Println(lib.PreviewString(args.Preview)+"s3 deleted:", key, args.Version)

	} else {

		var delimiter *string
		if !args.Recursive {
			delimiter = aws.String("/")
		}

		var keyMarker *string
		var versionMarker *string
		for {
			out, err := s3Client.ListObjectVersionsWithContext(ctx, &s3.ListObjectVersionsInput{
				Bucket:          aws.String(bucket),
				Prefix:          aws.String(key),
				Delimiter:       delimiter,
				KeyMarker:       keyMarker,
				VersionIdMarker: versionMarker,
			})
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}

			var objects []*s3.ObjectIdentifier

			for _, obj := range out.Versions {
				objects = append(objects, &s3.ObjectIdentifier{
					Key:       obj.Key,
					VersionId: obj.VersionId,
				})
			}

			for _, obj := range out.DeleteMarkers {
				objects = append(objects, &s3.ObjectIdentifier{
					Key:       obj.Key,
					VersionId: obj.VersionId,
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
						version := *err.VersionId
						if version == "" {
							version = "-"
						}
						fmt.Println("error:", *err.Key, version, *err.Code, *err.Message)
					}
					if len(deleteOut.Errors) != 0 {
						os.Exit(1)
					}

				}

				for _, object := range objects {
					version := *object.VersionId
					if version == "" {
						version = "-"
					}
					fmt.Println(lib.PreviewString(args.Preview)+"s3 deleted:", *object.Key, version)
				}

			}
			if !*out.IsTruncated {
				break
			}

			keyMarker = out.NextKeyMarker
			versionMarker = out.NextVersionIdMarker
		}
	}
}
