package lib

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/aws/aws-sdk-go/service/s3"
)

var s3Client *s3.S3
var s3ClientLock sync.RWMutex
var s3ClientsRegional = make(map[string]*s3.S3)

func S3Client() *s3.S3 {
	s3ClientLock.Lock()
	defer s3ClientLock.Unlock()
	if s3Client == nil {
		s3Client = s3.New(Session())
	}
	return s3Client
}

func S3ClientRegion(region string) (*s3.S3, error) {
	s3ClientLock.Lock()
	defer s3ClientLock.Unlock()
	s3Client, ok := s3ClientsRegional[region]
	if !ok {
		sess, err := SessionRegion(region)
		if err != nil {
			return nil, err
		}
		s3Client = s3.New(sess)
		s3ClientsRegional[region] = s3Client
	}
	return s3Client, nil
}

var s3BucketRegionLock sync.RWMutex
var s3BucketRegion = make(map[string]string)

func S3BucketRegion(bucket string) (string, error) {
	s3BucketRegionLock.Lock()
	defer s3BucketRegionLock.Unlock()
	region, ok := s3BucketRegion[bucket]
	if !ok {
		resp, err := http.Head(fmt.Sprintf("https://%s.s3.amazonaws.com", bucket))
		if err != nil {
			return "", err
		}
		err = resp.Body.Close()
		if err != nil {
			return "", err
		}
		region = resp.Header.Get("x-amz-bucket-region")
		if region == "" {
			return "", fmt.Errorf("empty x-amz-bucket-region for bucket: %s", bucket)
		}
		s3BucketRegion[bucket] = region
	}
	return region, nil
}

func S3ClientBucketRegion(bucket string) (*s3.S3, error) {
	region, err := S3BucketRegion(bucket)
	if err != nil {
		return nil, err
	}
	s3Client, err := S3ClientRegion(region)
	if err != nil {
		return nil, err
	}
	return s3Client, nil
}
