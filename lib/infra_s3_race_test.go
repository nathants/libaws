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
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
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

func TestInfraListSetRejectsUnsupportedBucketLifecycle(t *testing.T) {
	for _, lifecycle := range []string{
		`<LifecycleConfiguration><Rule><ID>date</ID><Status>Enabled</Status><Filter><Prefix/></Filter><Expiration><Date>2026-12-01T00:00:00Z</Date></Expiration></Rule></LifecycleConfiguration>`,
		`<LifecycleConfiguration><Rule><ID>obsolete-prefix</ID><Status>Enabled</Status><Prefix/><Expiration><Days>7</Days></Expiration></Rule></LifecycleConfiguration>`,
	} {
		t.Run(lifecycle, func(t *testing.T) {
			const bucket = "selected-date-lifecycle"
			installInfraListS3TestClient(t, infraListS3RoundTripFunc(func(r *http.Request) (*http.Response, error) {
				status, body := 200, ""
				switch {
				case r.URL.Path == "/":
					body = infraListS3ListBucketsXML(bucket)
				case r.URL.Query().Has("tagging"):
					body = `<Tagging><TagSet><Tag><Key>libaws.infraset</Key><Value>wanted</Value></Tag></TagSet></Tagging>`
				case r.URL.Query().Has("versioning"):
					body = `<VersioningConfiguration/>`
				case r.URL.Query().Has("acl"):
					body = `<AccessControlPolicy/>`
				case r.URL.Query().Has("cors"):
					status, body = 404, `<Error><Code>NoSuchCORSConfiguration</Code></Error>`
				case r.URL.Query().Has("encryption"):
					body = `<ServerSideEncryptionConfiguration><Rule><ApplyServerSideEncryptionByDefault><SSEAlgorithm>AES256</SSEAlgorithm></ApplyServerSideEncryptionByDefault><BlockedEncryptionTypes><EncryptionType>SSE-C</EncryptionType></BlockedEncryptionTypes><BucketKeyEnabled>false</BucketKeyEnabled></Rule></ServerSideEncryptionConfiguration>`
				case r.URL.Query().Has("lifecycle"):
					body = lifecycle
				case r.URL.Query().Has("logging"):
					body = `<BucketLoggingStatus/>`
				case r.URL.Query().Has("notification"):
					body = `<NotificationConfiguration/>`
				case r.URL.Query().Has("policy"):
					status, body = 404, `<Error><Code>NoSuchBucketPolicy</Code></Error>`
				case r.URL.Query().Has("replication"):
					status, body = 404, `<Error><Code>ReplicationConfigurationNotFoundError</Code></Error>`
				case r.URL.Query().Has("metrics"):
					status, body = 404, `<Error><Code>NoSuchConfiguration</Code></Error>`
				default:
					return nil, fmt.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				return infraListS3Response(r, status, body), nil
			}), true)
			s3BucketRegionLock.Lock()
			s3BucketRegion[bucket] = "us-east-1"
			s3BucketRegionLock.Unlock()
			_, err := (infraListScope{setName: "wanted"}).listS3(context.Background(), make(chan *InfraTrigger, 1))
			if err == nil || !strings.Contains(err.Error(), "unsupported lifecycle configuration") {
				t.Fatalf("expected an explicit representability error for unsupported lifecycle: %v", err)
			}
		})
	}
}

func TestInfraListS3LifecycleRepresentation(t *testing.T) {
	for _, scenario := range []string{"none", "days", "empty-filter", "filter-prefix", "disabled", "no-expiration", "delete-marker", "transition", "multiple"} {
		t.Run(scenario, func(t *testing.T) {
			rule := s3types.LifecycleRule{ID: aws.String("arbitrary"), Status: s3types.ExpirationStatusEnabled, Expiration: &s3types.LifecycleExpiration{Days: aws.Int32(7)}}
			valid := true
			switch scenario {
			case "none", "days":
			case "empty-filter":
				rule.Filter = &s3types.LifecycleRuleFilter{Prefix: aws.String("")}
			case "filter-prefix":
				rule.Filter = &s3types.LifecycleRuleFilter{Prefix: aws.String("part/")}
				valid = false
			case "disabled":
				rule.Status = s3types.ExpirationStatusDisabled
				valid = false
			case "no-expiration":
				rule.Expiration = nil
				valid = false
			case "delete-marker":
				rule.Expiration = &s3types.LifecycleExpiration{ExpiredObjectDeleteMarker: aws.Bool(true)}
				valid = false
			case "transition":
				rule.Transitions = []s3types.Transition{{Days: aws.Int32(3), StorageClass: s3types.TransitionStorageClassGlacier}}
				valid = false
			case "multiple":
				valid = false
			default:
				t.Fatal("unknown lifecycle case")
			}
			rules := []s3types.LifecycleRule{rule}
			want := int32(7)
			if scenario == "none" {
				rules = nil
				want = 0
			}
			if scenario == "multiple" {
				rules = append(rules, rule)
			}
			days, err := s3LifecycleTTLDays("fixture", rules)
			if valid {
				if err != nil || days != want {
					t.Fatalf("representable lifecycle: days=%d err=%v", days, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "unsupported lifecycle configuration") {
				t.Fatalf("unrepresentable lifecycle accepted: days=%d err=%v", days, err)
			}
		})
	}
}
