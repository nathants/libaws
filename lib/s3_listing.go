package lib

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3SplitPath accepts bucket/key or s3://bucket/key. Keys are opaque strings.
func S3SplitPath(value string) (string, string, error) {
	bucket, key, err := SplitOnce(strings.TrimPrefix(value, "s3://"), "/")
	if err != nil {
		return "", "", err
	}
	if bucket == "" {
		return "", "", errors.New("S3 bucket is required")
	}
	return bucket, key, nil
}

func s3ListingParent(key string) string {
	return key[:strings.LastIndex(key, "/")+1]
}

type S3ListInput struct {
	Path       string
	Quiet      bool
	Recursive  bool
	StartAfter bool
}

// S3List writes bucket names, object keys, and common prefixes.
func S3List(ctx context.Context, input *S3ListInput, output io.Writer) error {
	if input == nil || output == nil {
		return errors.New("S3 list input and output writer must not be nil")
	}
	value := strings.TrimPrefix(input.Path, "s3://")
	if !strings.Contains(value, "/") {
		out, err := S3Client().ListBuckets(ctx, &s3.ListBucketsInput{})
		if err != nil {
			return err
		}
		for _, bucket := range out.Buckets {
			name := aws.ToString(bucket.Name)
			if !strings.HasPrefix(name, value) {
				continue
			}
			if input.Quiet {
				name += "/"
			}
			if _, err := fmt.Fprintln(output, name); err != nil {
				return err
			}
		}
		return nil
	}
	bucket, key, err := S3SplitPath(input.Path)
	if err != nil {
		return err
	}
	client, err := S3ClientBucketRegion(ctx, bucket)
	if err != nil {
		return err
	}
	request := &s3.ListObjectsV2Input{Bucket: aws.String(bucket)}
	if input.StartAfter {
		request.StartAfter = aws.String(key)
	} else {
		request.Prefix = aws.String(key)
		if !input.Recursive {
			request.Delimiter = aws.String("/")
		}
	}
	parent := s3ListingParent(key)
	for {
		out, err := client.ListObjectsV2(ctx, request)
		if err != nil {
			return err
		}
		for _, prefix := range out.CommonPrefixes {
			name := aws.ToString(prefix.Prefix)
			line := " PRE " + strings.TrimPrefix(name, parent)
			if input.Quiet {
				line = bucket + "/" + name
			}
			if _, err := fmt.Fprintln(output, line); err != nil {
				return err
			}
		}
		for _, object := range out.Contents {
			name := aws.ToString(object.Key)
			if input.Quiet {
				if _, err := fmt.Fprintln(output, bucket+"/"+name); err != nil {
					return err
				}
				continue
			}
			if request.Delimiter != nil {
				name = strings.TrimPrefix(name, parent)
			}
			if _, err := fmt.Fprintln(output, object.LastModified.In(time.Local).Format("2006-01-02 15:04:05"), fmt.Sprintf("%10v", aws.ToInt64(object.Size)), name, object.StorageClass); err != nil {
				return err
			}
		}
		if !aws.ToBool(out.IsTruncated) {
			return nil
		}
		request.ContinuationToken = out.NextContinuationToken
	}
}
