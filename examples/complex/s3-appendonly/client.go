package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func main() {
	if len(os.Args) < 4 {
		panic("usage: client ACTION BUCKET KEY [VERSION]")
	}
	action, bucket, key := os.Args[1], os.Args[2], os.Args[3]
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		panic(err)
	}
	client := s3.NewFromConfig(cfg)
	switch action {
	case "create":
		_, err = client.PutObject(ctx, &s3.PutObjectInput{
			Bucket:               aws.String(bucket),
			Key:                  aws.String(key),
			Body:                 strings.NewReader("original"),
			IfNoneMatch:          aws.String("*"),
			ServerSideEncryption: s3types.ServerSideEncryptionAes256,
		})
	case "overwrite":
		_, err = client.PutObject(ctx, &s3.PutObjectInput{
			Bucket:               aws.String(bucket),
			Key:                  aws.String(key),
			Body:                 strings.NewReader("replacement"),
			ServerSideEncryption: s3types.ServerSideEncryptionAes256,
		})
	case "get":
		var out *s3.GetObjectOutput
		out, err = client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
		if err == nil {
			var data []byte
			data, err = io.ReadAll(out.Body)
			closeErr := out.Body.Close()
			if err == nil {
				err = closeErr
			}
			if err == nil {
				fmt.Print(string(data))
			}
		}
	case "list":
		var out *s3.ListObjectsV2Output
		out, err = client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(bucket)})
		if err == nil {
			for _, object := range out.Contents {
				fmt.Println(aws.ToString(object.Key))
			}
		}
	case "version":
		var out *s3.HeadObjectOutput
		out, err = client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
		if err == nil {
			fmt.Print(aws.ToString(out.VersionId))
		}
	case "delete":
		_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	case "delete-version":
		if len(os.Args) != 5 {
			panic("delete-version requires VERSION")
		}
		_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket:    aws.String(bucket),
			Key:       aws.String(key),
			VersionId: aws.String(os.Args[4]),
		})
	case "copy":
		_, err = client.CopyObject(ctx, &s3.CopyObjectInput{
			Bucket:     aws.String(bucket),
			Key:        aws.String(key),
			CopySource: aws.String(url.PathEscape(bucket + "/" + key)),
		})
	default:
		panic("unknown action: " + action)
	}
	if err != nil {
		panic(err)
	}
}
