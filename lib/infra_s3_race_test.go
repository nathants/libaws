package lib

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type infraListS3RoundTripFunc func(*http.Request) (*http.Response, error)

func (function infraListS3RoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func infraListS3Response(request *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/xml"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}

func infraListS3ListBucketsXML(bucket string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">` +
		`<Buckets><Bucket><Name>` + bucket + `</Name><CreationDate>2026-01-01T00:00:00Z</CreationDate></Bucket></Buckets>` +
		`</ListAllMyBucketsResult>`
}

func infraListS3NoSuchBucketXML(bucket string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<Error><Code>NoSuchBucket</Code><Message>The specified bucket does not exist</Message>` +
		`<BucketName>` + bucket + `</BucketName><RequestId>request-id</RequestId><HostId>host-id</HostId></Error>`
}

func installInfraListS3TestClient(t *testing.T, transport http.RoundTripper, bucketOverride bool) {
	t.Helper()
	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider("access-key", "secret-key", "")),
		HTTPClient:  &http.Client{Transport: transport},
	}, func(options *s3.Options) {
		options.BaseEndpoint = aws.String("https://s3.test")
		options.UsePathStyle = true
	})

	s3ClientLock.Lock()
	originalClient := s3Client
	originalRegionalClients := s3ClientsRegional
	originalBucketOverride := s3BucketClientOverride
	s3Client = client
	s3ClientsRegional = map[string]*s3.Client{}
	if bucketOverride {
		s3BucketClientOverride = client
	} else {
		s3BucketClientOverride = nil
	}
	s3ClientLock.Unlock()

	s3BucketRegionLock.Lock()
	originalRegions := s3BucketRegion
	s3BucketRegion = map[string]string{}
	s3BucketRegionLock.Unlock()

	t.Cleanup(func() {
		s3ClientLock.Lock()
		s3Client = originalClient
		s3ClientsRegional = originalRegionalClients
		s3BucketClientOverride = originalBucketOverride
		s3ClientLock.Unlock()

		s3BucketRegionLock.Lock()
		s3BucketRegion = originalRegions
		s3BucketRegionLock.Unlock()
	})
}

func TestInfraListS3SkipsBucketDeletedAfterList(t *testing.T) {
	const bucket = "libaws-deleted-after-list"

	t.Run("region resolution", func(t *testing.T) {
		installInfraListS3TestClient(t, infraListS3RoundTripFunc(func(request *http.Request) (*http.Response, error) {
			switch {
			case request.Method == http.MethodGet && request.URL.Path == "/":
				return infraListS3Response(request, http.StatusOK, infraListS3ListBucketsXML(bucket)), nil
			case request.Method == http.MethodHead && request.URL.Path == "/"+bucket:
				return infraListS3Response(request, http.StatusNotFound, ""), nil
			default:
				return nil, fmt.Errorf("unexpected S3 request: %s %s", request.Method, request.URL)
			}
		}), false)

		buckets, err := InfraListS3(context.Background(), make(chan *InfraTrigger, 1))
		if err != nil {
			t.Fatalf("InfraListS3 returned a deletion race error: %v", err)
		}
		if len(buckets) != 0 {
			t.Fatalf("InfraListS3 returned deleted bucket: %#v", buckets)
		}
	})

	t.Run("tagging", func(t *testing.T) {
		installInfraListS3TestClient(t, infraListS3RoundTripFunc(func(request *http.Request) (*http.Response, error) {
			switch {
			case request.URL.Path == "/":
				return infraListS3Response(request, http.StatusOK, infraListS3ListBucketsXML(bucket)), nil
			case request.URL.Query().Has("tagging"):
				return infraListS3Response(request, http.StatusNotFound, infraListS3NoSuchBucketXML(bucket)), nil
			default:
				return nil, fmt.Errorf("unexpected S3 request: %s", request.URL)
			}
		}), true)

		buckets, err := InfraListS3(context.Background(), make(chan *InfraTrigger, 1))
		if err != nil {
			t.Fatalf("InfraListS3 returned a deletion race error: %v", err)
		}
		if len(buckets) != 0 {
			t.Fatalf("InfraListS3 returned deleted bucket: %#v", buckets)
		}
	})

	t.Run("description", func(t *testing.T) {
		installInfraListS3TestClient(t, infraListS3RoundTripFunc(func(request *http.Request) (*http.Response, error) {
			switch {
			case request.URL.Path == "/":
				return infraListS3Response(request, http.StatusOK, infraListS3ListBucketsXML(bucket)), nil
			case request.URL.Query().Has("tagging"):
				return infraListS3Response(request, http.StatusOK, `<Tagging xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><TagSet/></Tagging>`), nil
			case request.URL.Query().Has("versioning"):
				return infraListS3Response(request, http.StatusNotFound, infraListS3NoSuchBucketXML(bucket)), nil
			default:
				return nil, fmt.Errorf("unexpected S3 request: %s", request.URL)
			}
		}), true)

		buckets, err := InfraListS3(context.Background(), make(chan *InfraTrigger, 1))
		if err != nil {
			t.Fatalf("InfraListS3 returned a deletion race error: %v", err)
		}
		if len(buckets) != 0 {
			t.Fatalf("InfraListS3 returned deleted bucket: %#v", buckets)
		}
	})
}

func TestInfraListSetSkipsUnrelatedBucketConfiguration(t *testing.T) {
	for _, setName := range []string{"wanted", "wanted-suffix", "", "other"} {
		t.Run("tag="+setName, func(t *testing.T) {
			installInfraListS3TestClient(t, infraListS3RoundTripFunc(func(request *http.Request) (*http.Response, error) {
				switch {
				case request.URL.Path == "/":
					return infraListS3Response(request, http.StatusOK, infraListS3ListBucketsXML("cross-named-bucket")), nil
				case request.URL.Query().Has("tagging"):
					return infraListS3Response(request, http.StatusOK, `<Tagging><TagSet><Tag><Key>libaws.infraset</Key><Value>`+setName+`</Value></Tag></TagSet></Tagging>`), nil
				default:
					return infraListS3Response(request, http.StatusForbidden, `<Error><Code>AccessDenied</Code><Message>configuration probe</Message></Error>`), nil
				}
			}), true)
			buckets, err := (infraListScope{setName: "wanted"}).listS3(context.Background(), make(chan *InfraTrigger, 1))
			if setName == "wanted" {
				if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
					t.Fatalf("selected bucket's real error was lost: %v", err)
				}
			} else if err != nil || len(buckets) != 0 {
				t.Fatalf("unrelated bucket was described: buckets=%v err=%v", buckets, err)
			}
			// The global API must remain strict for the very same bucket.
			if _, err := InfraListS3(context.Background(), make(chan *InfraTrigger, 1)); err == nil {
				t.Fatal("global inventory stopped inspecting bucket configuration")
			}
		})
	}
}

func TestInfraListS3PreservesOtherBucketErrors(t *testing.T) {
	const bucket = "libaws-access-denied-after-list"
	installInfraListS3TestClient(t, infraListS3RoundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch {
		case request.URL.Path == "/":
			return infraListS3Response(request, http.StatusOK, infraListS3ListBucketsXML(bucket)), nil
		case request.URL.Query().Has("tagging"):
			body := `<Error><Code>AccessDenied</Code><Message>Access denied</Message><RequestId>request-id</RequestId><HostId>host-id</HostId></Error>`
			return infraListS3Response(request, http.StatusForbidden, body), nil
		default:
			return nil, fmt.Errorf("unexpected S3 request: %s", request.URL)
		}
	}), true)

	buckets, err := InfraListS3(context.Background(), make(chan *InfraTrigger, 1))
	if err == nil {
		t.Fatalf("InfraListS3 ignored AccessDenied and returned buckets: %#v", buckets)
	}
	if !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("InfraListS3 returned wrong error: %v", err)
	}
}

func TestInfraListSetPreservesSelectedBucketDisappearance(t *testing.T) {
	const bucket = "cross-named-bucket"
	installInfraListS3TestClient(t, infraListS3RoundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch {
		case request.URL.Path == "/":
			return infraListS3Response(request, 200, infraListS3ListBucketsXML(bucket)), nil
		case request.URL.Query().Has("tagging"):
			return infraListS3Response(request, 200, `<Tagging><TagSet><Tag><Key>libaws.infraset</Key><Value>wanted</Value></Tag></TagSet></Tagging>`), nil
		default:
			return infraListS3Response(request, 404, infraListS3NoSuchBucketXML(bucket)), nil
		}
	}), true)
	if _, err := (infraListScope{setName: "wanted"}).listS3(context.Background(), make(chan *InfraTrigger, 1)); err == nil {
		t.Fatal("lost selected resource error")
	}
	if _, err := InfraListS3(context.Background(), make(chan *InfraTrigger, 1)); err != nil {
		t.Fatalf("changed global disappearance handling: %v", err)
	}
}
