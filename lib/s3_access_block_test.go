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
	"github.com/gofrs/uuid"
)

type s3AccessBlockTransport func(*http.Request) (*http.Response, error)

func (transport s3AccessBlockTransport) Do(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestS3EnsureRejectsMissingPublicAccessBlock(t *testing.T) {
	for _, body := range []string{
		`<Error><Code>NoSuchPublicAccessBlockConfiguration</Code></Error>`,
		`<PublicAccessBlockConfiguration/>`,
		`<PublicAccessBlockConfiguration><BlockPublicAcls>true</BlockPublicAcls></PublicAccessBlockConfiguration>`,
	} {
		for _, preview := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/preview=%v", body, preview), func(t *testing.T) {
				oldClient, oldAccount := s3Client, stsAccount
				t.Cleanup(func() { s3Client, stsAccount = oldClient, oldAccount })
				stsAccount = aws.String("123456789012")
				s3Client = s3.NewFromConfig(aws.Config{
					Region: "us-east-1", Credentials: aws.AnonymousCredentials{}, RetryMaxAttempts: 1,
					HTTPClient: s3AccessBlockTransport(func(request *http.Request) (*http.Response, error) {
						status, response := 200, ""
						switch {
						case request.Method == "HEAD":
						case request.Method == "GET" && request.URL.Query().Has("tagging"):
							response = `<Tagging><TagSet><Tag><Key>libaws.infraset</Key><Value>wanted</Value></Tag></TagSet></Tagging>`
						case request.Method == "GET" && request.URL.Query().Has("publicAccessBlock"):
							response = body
							if strings.Contains(body, "<Error>") {
								status = 404
							}
						default:
							return nil, fmt.Errorf("unexpected read or mutation: %s %s", request.Method, request.URL)
						}
						return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(response)), Request: request}, nil
					}),
				})
				input, err := S3EnsureInput("wanted", "fixture", nil)
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if failure := recover(); failure != nil {
						t.Fatalf("missing public access block caused panic: %v", failure)
					}
				}()
				err = S3Ensure(context.Background(), input, preview)
				if err == nil || !strings.Contains(err.Error(), "restore the original public access block") {
					t.Fatalf("expected actionable immutable-ACL error: %v", err)
				}
			})
		}
	}
}

func TestS3MissingPublicAccessBlockIntegration(t *testing.T) {
	requireLiveAWSAccount(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	bucket := "test-pab-" + uuid.Must(uuid.NewV4()).String()
	client := S3Client()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		if err := S3DeleteBucket(cleanupCtx, bucket, false); err != nil && !isS3NoSuchBucket(err) {
			t.Errorf("delete PAB fixture: %v", err)
		}
		if _, err := client.GetBucketLocation(cleanupCtx, &s3.GetBucketLocationInput{Bucket: aws.String(bucket)}); !isS3NoSuchBucket(err) {
			t.Errorf("PAB fixture remains or deletion could not be verified: %v", err)
		}
	})
	input, err := S3EnsureInput(bucket, bucket, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := S3Ensure(ctx, input, false); err != nil {
		t.Fatal(err)
	}
	// Empty private bucket, no public grants or objects; no account-level setting changes.
	if _, err := client.DeletePublicAccessBlock(ctx, &s3.DeletePublicAccessBlockInput{Bucket: aws.String(bucket)}); err != nil {
		t.Fatal(err)
	}
	if err := waitLiveAWSFixtureStable(ctx, func() (bool, error) {
		_, err := client.GetPublicAccessBlock(ctx, &s3.GetPublicAccessBlockInput{Bucket: aws.String(bucket)})
		if err != nil && strings.Contains(err.Error(), s3ErrCodeNoSuchPublicAccessBlockConfiguration) {
			return true, nil
		}
		return false, err
	}); err != nil {
		t.Fatal(err)
	}
	for _, preview := range []bool{true, false} {
		err := S3Ensure(ctx, input, preview)
		if err == nil || !strings.Contains(err.Error(), "restore the original public access block") {
			t.Fatalf("preview=%v expected actionable drift error: %v", preview, err)
		}
	}
	if _, err := client.GetPublicAccessBlock(ctx, &s3.GetPublicAccessBlockInput{Bucket: aws.String(bucket)}); err == nil || !strings.Contains(err.Error(), s3ErrCodeNoSuchPublicAccessBlockConfiguration) {
		t.Fatalf("ensure must not invent an ACL: %v", err)
	}
}
