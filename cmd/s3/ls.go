package libaws

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["s3-ls"] = s3Ls
	lib.Args["s3-ls"] = s3LsArgs{}
}

type s3LsArgs struct {
	Path       string `arg:"positional"`
	Quiet      bool   `arg:"-q,--quiet" help:"print key only"`
	Recursive  bool   `arg:"-r,--recursive" help:"list all keys with prefix path"`
	StartAfter bool   `arg:"-s,--start-after" help:"list all keys that lexically appear after path"`
}

func (s3LsArgs) Description() string {
	return "\nlist s3 content\n"
}

func s3Ls() {
	var args s3LsArgs
	arg.MustParse(&args)
	ctx := context.Background()

	if args.Path == "" || !strings.Contains(args.Path, "/") {
		out, err := lib.S3Client().ListBuckets(ctx, &s3.ListBucketsInput{})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		for _, bucket := range out.Buckets {
			if !strings.Contains(args.Path, "/") && !strings.HasPrefix(*bucket.Name, args.Path) {
				continue
			}
			if args.Quiet {
				fmt.Println(*bucket.Name + "/")
			} else {
				fmt.Println(*bucket.Name)
			}
		}
	} else {

		args.Path = strings.ReplaceAll(args.Path, "s3://", "")
		bucket, key, err := lib.SplitOnce(args.Path, "/")
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}

		splitKey := key
		if !strings.HasSuffix(key, "/") {
			splitKey = path.Dir(key) + "/"
			if splitKey == "./" {
				splitKey = ""
			}
		}

		s3Client, err := lib.S3ClientBucketRegion(bucket)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}

		var delimiter *string
		var startAfter *string
		var prefix *string
		if args.StartAfter {
			startAfter = aws.String(key)
		} else if args.Recursive {
			prefix = aws.String(key)
		} else {
			prefix = aws.String(key)
			delimiter = aws.String("/")
		}
		var token *string
		for {
			input := &s3.ListObjectsV2Input{
				Bucket:            aws.String(bucket),
				StartAfter:        startAfter,
				Prefix:            prefix,
				Delimiter:         delimiter,
				ContinuationToken: token,
			}
			out, err := s3Client.ListObjectsV2(ctx, input)
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}

			for _, pre := range out.CommonPrefixes {
				p := *pre.Prefix
				if splitKey != "" {
					p = strings.SplitN(p, splitKey, 2)[1]
				}
				if args.Quiet {
					path := args.Path
					if !strings.HasSuffix(args.Path, "/") {
						parts := strings.Split(args.Path, "/")
						path = strings.Join(parts[:len(parts)-1], "/") + "/"
					}
					fmt.Println(path + p)
				} else {
					fmt.Println(" PRE", p)
				}
			}

			zone, _ := time.Now().Zone()
			loc, err := time.LoadLocation(zone)
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}

			for _, obj := range out.Contents {
				objKey := *obj.Key
				if delimiter != nil && splitKey != "" {
					objKey = strings.SplitN(objKey, splitKey, 2)[1]
				}
				if args.Quiet {
					path := args.Path
					if !strings.HasSuffix(args.Path, "/") {
						parts := strings.Split(args.Path, "/")
						path = strings.Join(parts[:len(parts)-1], "/") + "/"
					}
					fmt.Println(path + objKey)
				} else {
					fmt.Println(
						fmt.Sprint(obj.LastModified.In(loc))[:19],
						fmt.Sprintf("%10v", *obj.Size),
						objKey,
						obj.StorageClass,
					)
				}
			}

			if !*out.IsTruncated {
				break
			}

			token = out.NextContinuationToken
		}
	}
}
