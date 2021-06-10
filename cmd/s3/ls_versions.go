package cliaws

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["s3-ls-versions"] = s3LsVersions
}

type s3LsVersionsArgs struct {
	Path      string `arg:"positional"`
	Recursive bool   `arg:"-r,--recursive"`
}

func (s3LsVersionsArgs) Description() string {
	return "\nlist s3 content versions\n"
}

func s3LsVersions() {
	var args s3LsVersionsArgs
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

			var objects []*S3ObjectVersion

			for _, obj := range out.Versions {
				objKey := *obj.Key
				if key != "" && !args.Recursive {
					objKey = strings.SplitN(objKey, key, 2)[1]
				}
				version := *obj.VersionId
				kind := "HISTORICAL"
				if *obj.IsLatest {
					kind = "LATEST"
				}
				objects = append(objects, &S3ObjectVersion{
					Date:         fmt.Sprint(obj.LastModified.In(loc))[:19],
					Size:         fmt.Sprintf("%10v", *obj.Size),
					Key:          objKey,
					StorageClass: *obj.StorageClass,
					Version:      version,
					Kind:         kind,
				})
			}

			for _, obj := range out.DeleteMarkers {
				objKey := *obj.Key
				if key != "" && !args.Recursive {
					objKey = strings.SplitN(objKey, key, 2)[1]
				}
				version := *obj.VersionId
				kind := "HISTORICAL-DELETE"
				if *obj.IsLatest {
					kind = "LATEST-DELETE"
				}
				objects = append(objects, &S3ObjectVersion{
					Date:         fmt.Sprint(obj.LastModified.In(loc))[:19],
					Size:         "-",
					Key:          objKey,
					StorageClass: "-",
					Version:      version,
					Kind:         kind,
				})
			}

			sort.SliceStable(objects, func(a, b int) bool { return objects[a].Date > objects[b].Date })
			sort.SliceStable(objects, func(a, b int) bool { return objects[a].Key < objects[b].Key })

			for _, obj := range objects {
				fmt.Println(
					obj.Date,
					obj.Size,
					obj.Key,
					obj.StorageClass,
					obj.Version,
					obj.Kind,
				)
			}

			if !*out.IsTruncated {
				break
			}
			keyMarker = out.NextKeyMarker
			versionMarker = out.NextVersionIdMarker
		}
	}
}

type S3ObjectVersion struct {
	Date         string
	Size         string
	Key          string
	StorageClass string
	Version      string
	Kind         string
}
