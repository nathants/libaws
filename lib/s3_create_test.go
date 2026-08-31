package lib

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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

func s3RegionResponse(request *http.Request, status int, region string) *http.Response {
	header := http.Header{}
	if region != "" {
		header.Set("X-Amz-Bucket-Region", region)
	}
	body := io.ReadCloser(http.NoBody)
	if status >= http.StatusMultipleChoices {
		header.Set("Content-Type", "application/xml")
		body = io.NopCloser(strings.NewReader(
			`<Error><Code>TestError</Code><Message>test error</Message></Error>`,
		))
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       body,
		Request:    request,
	}
}

func newS3RegionTestClient(region string, transport http.RoundTripper) *s3.Client {
	return s3.NewFromConfig(aws.Config{
		Region:      region,
		Credentials: aws.AnonymousCredentials{},
		HTTPClient:  &http.Client{Transport: transport},
	})
}

func installS3BucketRegionTestState(t *testing.T, client *s3.Client) {
	t.Helper()

	s3ClientLock.Lock()
	originalClient := s3Client
	s3Client = client
	s3ClientLock.Unlock()

	s3BucketRegionLock.Lock()
	originalRegions := s3BucketRegion
	s3BucketRegion = map[string]string{}
	s3BucketRegionLock.Unlock()

	t.Cleanup(func() {
		s3ClientLock.Lock()
		s3Client = originalClient
		s3ClientLock.Unlock()

		s3BucketRegionLock.Lock()
		s3BucketRegion = originalRegions
		s3BucketRegionLock.Unlock()
	})
}

func TestResolveS3BucketRegionUsesSDKEndpointRulesAndPathStyle(t *testing.T) {
	const (
		bucket = "bucket.with.dots"
		region = "cn-north-1"
	)
	client := newS3RegionTestClient(region, s3RegionRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if got, want := request.Method, http.MethodHead; got != want {
			return nil, fmt.Errorf("method = %q, want %q", got, want)
		}
		if got, want := request.URL.Hostname(), "s3.cn-north-1.amazonaws.com.cn"; got != want {
			return nil, fmt.Errorf("hostname = %q, want %q", got, want)
		}
		if got, want := request.URL.EscapedPath(), "/"+bucket; got != want {
			return nil, fmt.Errorf("path = %q, want %q", got, want)
		}
		if authorization := request.Header.Get("Authorization"); authorization != "" {
			return nil, fmt.Errorf("region probe unexpectedly signed: %q", authorization)
		}
		return s3RegionResponse(request, http.StatusMovedPermanently, region), nil
	}))

	got, err := resolveS3BucketRegion(context.Background(), client, bucket)
	if err != nil {
		t.Fatal(err)
	}
	if got != region {
		t.Fatalf("region = %q, want %q", got, region)
	}
}

func TestResolveS3BucketRegionReturnsTypedNoSuchBucket(t *testing.T) {
	client := newS3RegionTestClient("us-west-2", s3RegionRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		return s3RegionResponse(request, http.StatusNotFound, ""), nil
	}))

	region, err := resolveS3BucketRegion(context.Background(), client, "missing-bucket")
	if err == nil {
		t.Fatal("missing bucket returned no error")
	}
	if region != "" {
		t.Fatalf("missing bucket region = %q, want empty", region)
	}
	var missing *s3types.NoSuchBucket
	if !errors.As(err, &missing) {
		t.Fatalf("missing bucket error = %T %v, want *types.NoSuchBucket", err, err)
	}
	if !isS3NoSuchBucket(err) {
		t.Fatalf("missing bucket error was not recognized: %v", err)
	}
}

func TestS3BucketRegionResolvesDistinctBucketsConcurrently(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	client := newS3RegionTestClient("us-west-2", s3RegionRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		started <- strings.TrimPrefix(request.URL.Path, "/")
		select {
		case <-release:
			return s3RegionResponse(request, http.StatusOK, "us-west-2"), nil
		case <-request.Context().Done():
			return nil, request.Context().Err()
		}
	}))
	installS3BucketRegionTestState(t, client)

	type result struct {
		region string
		err    error
	}
	results := make(chan result, 2)
	for _, bucket := range []string{"region-concurrency-one", "region-concurrency-two"} {
		go func(bucket string) {
			region, err := S3BucketRegion(context.Background(), bucket)
			results <- result{region: region, err: err}
		}(bucket)
	}

	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case bucket := <-started:
			seen[bucket] = true
		case <-timer.C:
			close(release)
			for range 2 {
				<-results
			}
			t.Fatalf("distinct uncached bucket-region probes were serialized; started = %v", seen)
		}
	}
	close(release)
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.region != "us-west-2" {
			t.Fatalf("region = %q, want us-west-2", result.region)
		}
	}
}

func TestS3BucketRegionHonorsCallerCancellation(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	client := newS3RegionTestClient("us-west-2", s3RegionRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		started <- struct{}{}
		select {
		case <-request.Context().Done():
			return nil, request.Context().Err()
		case <-release:
			return s3RegionResponse(request, http.StatusOK, "us-west-2"), nil
		}
	}))
	installS3BucketRegionTestState(t, client)

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := S3BucketRegion(ctx, "region-cancellation")
		result <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		close(release)
		<-result
		t.Fatal("bucket-region probe did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled bucket-region probe error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		close(release)
		<-result
		t.Fatal("bucket-region probe ignored caller cancellation")
	}
}

func TestS3BucketRegionCachesInProcess(t *testing.T) {
	var requests atomic.Int32
	client := newS3RegionTestClient("us-west-2", s3RegionRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests.Add(1)
		return s3RegionResponse(request, http.StatusOK, "us-west-2"), nil
	}))
	installS3BucketRegionTestState(t, client)

	for range 2 {
		region, err := S3BucketRegion(context.Background(), "region-cache")
		if err != nil {
			t.Fatal(err)
		}
		if region != "us-west-2" {
			t.Fatalf("region = %q, want us-west-2", region)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("bucket-region requests = %d, want 1", got)
	}
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

	client := newS3RegionTestClient("us-west-2", s3RegionRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		return s3RegionResponse(request, http.StatusOK, "us-west-2"), nil
	}))
	installS3BucketRegionTestState(t, client)

	region, err := S3BucketRegion(context.Background(), bucket)
	if err != nil {
		t.Fatal(err)
	}
	if region != "us-west-2" {
		t.Fatalf("region = %q, want authenticated response region", region)
	}
}
