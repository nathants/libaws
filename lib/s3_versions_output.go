package lib

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type s3ObjectVersion struct {
	LastModified time.Time
	Size         string
	Key          string
	StorageClass string
	Version      string
	Kind         string
}

func formatS3VersionDate(value time.Time) string {
	return value.In(time.Local).Format(time.RFC3339Nano)
}

func sortS3ObjectVersions(objects []*s3ObjectVersion) {
	sort.SliceStable(objects, func(a, b int) bool {
		if objects[a].Key != objects[b].Key {
			return objects[a].Key < objects[b].Key
		}
		return objects[a].LastModified.After(objects[b].LastModified)
	})
}

// S3ListVersions writes S3 object and delete-marker versions.
func S3ListVersions(ctx context.Context, value string, recursive bool, output io.Writer) error {
	if output == nil {
		return errors.New("output writer must not be nil")
	}
	if value == "" {
		out, err := S3Client().ListBuckets(ctx, &s3.ListBucketsInput{})
		if err != nil {
			return err
		}
		for _, bucket := range out.Buckets {
			if _, err := fmt.Fprintln(output, *bucket.Name); err != nil {
				return err
			}
		}
		return nil
	}

	value = strings.ReplaceAll(value, "s3://", "")
	bucket, key, err := SplitOnce(value, "/")
	if err != nil {
		return err
	}

	splitKey := key
	if !strings.HasSuffix(key, "/") {
		splitKey = path.Dir(key) + "/"
		if splitKey == "./" {
			splitKey = ""
		}
	}

	client, err := S3ClientBucketRegion(bucket)
	if err != nil {
		return err
	}

	var delimiter *string
	if !recursive {
		delimiter = aws.String("/")
	}

	var keyMarker *string
	var versionMarker *string
	for {
		out, err := client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{
			Bucket:          aws.String(bucket),
			Prefix:          aws.String(key),
			Delimiter:       delimiter,
			KeyMarker:       keyMarker,
			VersionIdMarker: versionMarker,
		})
		if err != nil {
			return err
		}

		for _, pre := range out.CommonPrefixes {
			prefix := *pre.Prefix
			if splitKey != "" {
				prefix = strings.SplitN(prefix, splitKey, 2)[1]
			}
			if _, err := fmt.Fprintln(output, " PRE", prefix); err != nil {
				return err
			}
		}

		var objects []*s3ObjectVersion
		for _, obj := range out.Versions {
			objKey := *obj.Key
			if splitKey != "" && !recursive {
				objKey = strings.SplitN(objKey, splitKey, 2)[1]
			}
			kind := "HISTORICAL"
			if *obj.IsLatest {
				kind = "LATEST"
			}
			objects = append(objects, &s3ObjectVersion{
				LastModified: *obj.LastModified,
				Size:         fmt.Sprintf("%10v", *obj.Size),
				Key:          objKey,
				StorageClass: string(obj.StorageClass),
				Version:      *obj.VersionId,
				Kind:         kind,
			})
		}

		for _, obj := range out.DeleteMarkers {
			objKey := *obj.Key
			if splitKey != "" && !recursive {
				objKey = strings.SplitN(objKey, splitKey, 2)[1]
			}
			kind := "HISTORICAL-DELETE"
			if *obj.IsLatest {
				kind = "LATEST-DELETE"
			}
			objects = append(objects, &s3ObjectVersion{
				LastModified: *obj.LastModified,
				Size:         "-",
				Key:          objKey,
				StorageClass: "-",
				Version:      *obj.VersionId,
				Kind:         kind,
			})
		}

		sortS3ObjectVersions(objects)
		for _, obj := range objects {
			if _, err := fmt.Fprintln(
				output,
				formatS3VersionDate(obj.LastModified),
				obj.Size,
				obj.Key,
				obj.StorageClass,
				obj.Version,
				obj.Kind,
			); err != nil {
				return err
			}
		}

		if out.NextKeyMarker == nil && out.NextVersionIdMarker == nil {
			return nil
		}
		keyMarker = out.NextKeyMarker
		versionMarker = out.NextVersionIdMarker
	}
}
