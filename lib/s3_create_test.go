package lib

import (
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func TestS3CreateBucketConfiguration(t *testing.T) {
	t.Run("us-east-1 omits location constraint", func(t *testing.T) {
		if config := s3CreateBucketConfiguration("us-east-1"); config != nil {
			t.Fatalf("us-east-1 configuration = %#v, want nil", config)
		}
	})

	t.Run("other region includes location constraint", func(t *testing.T) {
		config := s3CreateBucketConfiguration("us-west-2")
		if config == nil {
			t.Fatal("us-west-2 configuration is nil")
		}
		if got, want := config.LocationConstraint, s3types.BucketLocationConstraint("us-west-2"); got != want {
			t.Fatalf("location constraint = %q, want %q", got, want)
		}
	})
}

type s3RegionRoundTripFunc func(*http.Request) (*http.Response, error)

func (function s3RegionRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestS3BucketRegionIgnoresLegacyDiskCache(t *testing.T) {
	bucket := fmt.Sprintf("libaws-region-cache-test-%d-%d", os.Getpid(), time.Now().UnixNano())
	legacyCache := "/tmp/aws.s3.bucket.region=" + bucket
	file, err := os.OpenFile(legacyCache, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	poison := []byte("attacker-controlled")
	if count, err := file.Write(poison); err != nil || count != len(poison) {
		_ = file.Close()
		_ = os.Remove(legacyCache)
		t.Fatalf("write legacy cache fixture: bytes=%d error=%v", count, err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(legacyCache)
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(legacyCache) })

	s3BucketRegionLock.Lock()
	originalRegions := s3BucketRegion
	originalClient := s3BucketRegionHTTPClient
	s3BucketRegion = map[string]string{}
	s3BucketRegionHTTPClient = &http.Client{Transport: s3RegionRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusMovedPermanently,
			Header:     http.Header{"X-Amz-Bucket-Region": []string{"us-west-2"}},
			Body:       http.NoBody,
			Request:    request,
		}, nil
	})}
	s3BucketRegionLock.Unlock()
	t.Cleanup(func() {
		s3BucketRegionLock.Lock()
		defer s3BucketRegionLock.Unlock()
		s3BucketRegion = originalRegions
		s3BucketRegionHTTPClient = originalClient
	})

	region, err := S3BucketRegion(bucket)
	if err != nil {
		t.Fatal(err)
	}
	if region != "us-west-2" {
		t.Fatalf("region = %q, want authenticated response region", region)
	}
}
