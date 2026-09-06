package lib

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/gofrs/uuid"
)

type s3DeleteTransport func(*http.Request) (*http.Response, error)

func (transport s3DeleteTransport) Do(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestS3DeleteHistoricalNullVersion(t *testing.T) {
	for _, preview := range []bool{false, true} {
		t.Run(fmt.Sprintf("preview=%v", preview), func(t *testing.T) {
			deletedNull := false
			var logs strings.Builder
			oldClient, oldLogger := s3BucketClientOverride, *Logger
			t.Cleanup(func() { s3BucketClientOverride, *Logger = oldClient, oldLogger })
			Logger.disabled = false
			Logger.Print = func(args ...any) { fmt.Fprint(&logs, args...) }
			s3BucketClientOverride = s3.NewFromConfig(aws.Config{
				Region: "us-east-1", Credentials: aws.AnonymousCredentials{}, RetryMaxAttempts: 1,
				HTTPClient: s3DeleteTransport(func(request *http.Request) (*http.Response, error) {
					status, body := 200, ""
					switch {
					case request.Method == "GET" && request.URL.Query().Has("policy"):
						status, body = 404, `<Error><Code>NoSuchBucketPolicy</Code></Error>`
					case request.Method == "GET" && request.URL.Query().Get("list-type") == "2":
						body = `<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>`
					case request.Method == "GET" && request.URL.Query().Has("versions"):
						body = `<ListVersionsResult><IsTruncated>false</IsTruncated><Version><Key>old-key</Key><VersionId>null</VersionId><IsLatest>false</IsLatest></Version><DeleteMarker><Key>old-key</Key><VersionId>marker</VersionId><IsLatest>true</IsLatest></DeleteMarker></ListVersionsResult>`
					case !preview && request.Method == "POST" && request.URL.Query().Has("delete"):
						data, err := io.ReadAll(request.Body)
						if err != nil {
							return nil, err
						}
						deletedNull = strings.Contains(string(data), "<VersionId>null</VersionId>")
						if !strings.Contains(string(data), "<VersionId>marker</VersionId>") {
							t.Error("delete marker was not removed")
						}
						body = `<DeleteResult/>`
					case !preview && request.Method == "DELETE":
						if !deletedNull {
							status, body = 409, `<Error><Code>BucketNotEmpty</Code></Error>`
						}
					default:
						return nil, fmt.Errorf("unexpected request: %s %s", request.Method, request.URL)
					}
					return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
				}),
			})
			if err := S3DeleteBucket(context.Background(), "fixture", preview); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(logs.String(), "deleted version: old-key null") {
				t.Fatalf("null version omitted from teardown: %s", logs.String())
			}
		})
	}
}

func TestS3DeleteHistoricalNullVersionIntegration(t *testing.T) {
	requireLiveAWSAccount(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	bucket := "test-null-" + uuid.Must(uuid.NewV4()).String()
	client := S3Client()
	var marker *string
	deleted := false
	t.Cleanup(func() {
		if deleted {
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		// Exact owned IDs allow cleanup even if the teardown under test regresses.
		for _, version := range []*string{aws.String("null"), marker} {
			if version == nil {
				continue
			}
			_, err := client.DeleteObject(cleanupCtx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String("old-key"), VersionId: version})
			if err != nil && !isS3NoSuchBucket(err) {
				t.Errorf("cleanup version: %v", err)
			}
		}
		if err := S3DeleteBucket(cleanupCtx, bucket, false); err != nil && !isS3NoSuchBucket(err) {
			t.Errorf("cleanup bucket: %v", err)
		}
	})
	if _, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket), CreateBucketConfiguration: s3CreateBucketConfiguration(Region()),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String("old-key"), Body: strings.NewReader("historical unversioned data")}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket: aws.String(bucket), VersioningConfiguration: &s3types.VersioningConfiguration{Status: s3types.BucketVersioningStatusEnabled},
	}); err != nil {
		t.Fatal(err)
	}
	out, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String("old-key")})
	if err != nil {
		t.Fatal(err)
	}
	marker = out.VersionId
	versions, err := client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: aws.String(bucket)})
	if err != nil {
		t.Fatal(err)
	}
	if len(versions.Versions) != 1 || aws.ToString(versions.Versions[0].VersionId) != "null" || len(versions.DeleteMarkers) != 1 || !aws.ToBool(versions.DeleteMarkers[0].IsLatest) {
		t.Fatalf("fixture must have a null version hidden by a marker: %s", Json(versions))
	}
	if err := S3DeleteBucket(ctx, bucket, false); err != nil {
		t.Fatal(err)
	}
	// DeleteBucket acceptance can precede control-plane read convergence.
	if err := s3.NewBucketNotExistsWaiter(client).Wait(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucket)}, time.Minute); err != nil {
		t.Fatalf("wait for deleted bucket: %v", err)
	}
	_, err = client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: aws.String(bucket)})
	if !isS3NoSuchBucket(err) {
		t.Fatalf("bucket remains or deletion could not be verified: %v", err)
	}
	deleted = true
}
