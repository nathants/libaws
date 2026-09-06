package lib

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type s3ListingTransport func(*http.Request) (*http.Response, error)

func (transport s3ListingTransport) Do(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestS3ListVersionsPreservesLiteralKeys(t *testing.T) {
	for _, key := range []string{"a/./b/c", "a/../b/c", "a//b/c", "a/s3://b/c"} {
		for _, recursive := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/recursive=%v", key, recursive), func(t *testing.T) {
				old := s3BucketClientOverride
				t.Cleanup(func() { s3BucketClientOverride = old })
				s3BucketClientOverride = s3.NewFromConfig(aws.Config{
					Region: "us-east-1", Credentials: aws.AnonymousCredentials{}, RetryMaxAttempts: 1,
					HTTPClient: s3ListingTransport(func(request *http.Request) (*http.Response, error) {
						if !request.URL.Query().Has("versions") || request.URL.Query().Get("prefix") != key {
							return nil, fmt.Errorf("literal prefix changed: %s", request.URL)
						}
						body := `<ListVersionsResult><IsTruncated>false</IsTruncated><Version><Key>` + key + `</Key><VersionId>old</VersionId><IsLatest>false</IsLatest><LastModified>2026-01-01T00:00:00Z</LastModified><Size>3</Size><StorageClass>STANDARD</StorageClass></Version><DeleteMarker><Key>` + key + `</Key><VersionId>deleted</VersionId><IsLatest>true</IsLatest><LastModified>2026-01-02T00:00:00Z</LastModified></DeleteMarker></ListVersionsResult>`
						return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
					}),
				})
				defer func() {
					if failure := recover(); failure != nil {
						t.Fatalf("literal S3 key caused panic: %v", failure)
					}
				}()
				var output strings.Builder
				if err := S3ListVersions(context.Background(), "s3://fixture/"+key, recursive, &output); err != nil {
					t.Fatal(err)
				}
				wantKey := "c"
				if recursive {
					wantKey = key
				}
				lines := strings.Split(strings.TrimSpace(output.String()), "\n")
				if len(lines) != 2 || !strings.Contains(lines[0], " "+wantKey+" - deleted LATEST-DELETE") || !strings.Contains(lines[1], " "+wantKey+" STANDARD old HISTORICAL") {
					t.Fatalf("wrong literal version output: %s", output.String())
				}
			})
		}
	}
}

func TestS3ListPreservesLiteralKeys(t *testing.T) {
	for _, key := range []string{"a/./b/c", "a/../b/c", "a//b/c", "a/s3://b/c"} {
		for _, quiet := range []bool{false, true} {
			for _, mode := range []string{"prefix", "recursive", "start-after"} {
				t.Run(fmt.Sprintf("%s/quiet=%v/%s", key, quiet, mode), func(t *testing.T) {
					old := s3BucketClientOverride
					t.Cleanup(func() { s3BucketClientOverride = old })
					reads := 0
					s3BucketClientOverride = s3.NewFromConfig(aws.Config{
						Region: "us-east-1", Credentials: aws.AnonymousCredentials{}, RetryMaxAttempts: 1,
						HTTPClient: s3ListingTransport(func(request *http.Request) (*http.Response, error) {
							query := request.URL.Query()
							filter := "prefix"
							if mode == "start-after" {
								filter = "start-after"
								if query.Has("prefix") {
									return nil, fmt.Errorf("unexpected prefix for start-after")
								}
							}
							if query.Get(filter) != key || (query.Get("delimiter") == "/") != (mode == "prefix") {
								return nil, fmt.Errorf("incorrect listing input: %s", request.URL)
							}
							reads++
							if reads > 2 || (reads == 2 && query.Get("continuation-token") != "next") {
								return nil, fmt.Errorf("incorrect pagination: %s", request.URL)
							}
							body := `<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>`
							if reads == 1 {
								body = `<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>next</NextContinuationToken><Contents><Key>` + key + `-next</Key><LastModified>2026-01-01T00:00:00Z</LastModified><Size>3</Size><StorageClass>STANDARD</StorageClass></Contents>`
								if mode == "prefix" {
									body += `<CommonPrefixes><Prefix>` + key + `-dir/</Prefix></CommonPrefixes>`
								}
								body += `</ListBucketResult>`
							}
							return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
						}),
					})
					var output strings.Builder
					err := S3List(context.Background(), &S3ListInput{Path: "s3://fixture/" + key, Quiet: quiet, Recursive: mode == "recursive", StartAfter: mode == "start-after"}, &output)
					if err != nil {
						t.Fatal(err)
					}
					wantKey := key + "-next"
					if quiet {
						wantKey = "fixture/" + wantKey
					} else if mode == "prefix" {
						wantKey = "c-next"
					}
					if reads != 2 || !strings.Contains(output.String(), wantKey) {
						t.Fatalf("incorrect paginated output: reads=%d output=%s", reads, output.String())
					}
					if quiet {
						want := "fixture/" + key + "-next\n"
						if mode == "prefix" {
							want = "fixture/" + key + "-dir/\n" + want
						}
						if output.String() != want {
							t.Fatalf("quiet key duplicated or changed: %q want %q", output.String(), want)
						}
					} else if mode == "prefix" && !strings.Contains(output.String(), " PRE c-dir/\n") {
						t.Fatalf("incorrect common prefix: %q", output.String())
					}
				})
			}
		}
	}
}
