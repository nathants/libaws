package lib

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

const s3BucketRegionTimeout = 30 * time.Second

type s3HeadBucketClient interface {
	HeadBucket(
		context.Context,
		*s3.HeadBucketInput,
		...func(*s3.Options),
	) (*s3.HeadBucketOutput, error)
}

func resolveS3BucketRegion(ctx context.Context, client s3HeadBucketClient, bucket string) (string, error) {
	output, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucket)}, func(options *s3.Options) {
		options.Credentials = nil
		options.UsePathStyle = true
	})
	if output != nil {
		if region := aws.ToString(output.BucketRegion); region != "" {
			return region, nil
		}
	}
	if err != nil {
		var responseError *smithyhttp.ResponseError
		if errors.As(err, &responseError) && responseError.HTTPResponse() != nil {
			if region := responseError.HTTPResponse().Header.Get("X-Amz-Bucket-Region"); region != "" {
				return region, nil
			}
		}
		var statusError interface{ HTTPStatusCode() int }
		if errors.As(err, &statusError) && statusError.HTTPStatusCode() == http.StatusNotFound {
			return "", &s3types.NoSuchBucket{Message: aws.String("no such bucket: " + bucket)}
		}
		return "", err
	}
	return "", fmt.Errorf("empty S3 bucket region for bucket: %s", bucket)
}

var s3BucketRegionLock sync.Mutex
var s3BucketRegion = map[string]string{}

func S3BucketRegion(ctx context.Context, bucket string) (string, error) {
	s3BucketRegionLock.Lock()
	region, ok := s3BucketRegion[bucket]
	s3BucketRegionLock.Unlock()
	if ok {
		return region, nil
	}
	if doDebug {
		debug := &Debug{start: time.Now(), name: "S3BucketRegion"}
		debug.Start()
		defer debug.End()
	}

	probeCtx, cancel := context.WithTimeout(ctx, s3BucketRegionTimeout)
	defer cancel()
	region, err := resolveS3BucketRegion(probeCtx, S3Client(), bucket)
	if err != nil {
		return "", err
	}

	s3BucketRegionLock.Lock()
	if cached, ok := s3BucketRegion[bucket]; ok {
		region = cached
	} else {
		s3BucketRegion[bucket] = region
	}
	s3BucketRegionLock.Unlock()
	return region, nil
}

func S3ClientBucketRegion(ctx context.Context, bucket string) (*s3.Client, error) {
	if client := configuredS3BucketClientOverride(); client != nil {
		return client, nil
	}
	region, err := S3BucketRegion(ctx, bucket)
	if err != nil {
		return nil, err
	}
	return S3ClientRegion(region)
}

func S3ClientBucketRegionMust(ctx context.Context, bucket string) *s3.Client {
	client, err := S3ClientBucketRegion(ctx, bucket)
	if err != nil {
		panic(err)
	}
	return client
}
