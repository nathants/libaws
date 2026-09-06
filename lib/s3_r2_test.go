package lib

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// The opt-in R2 example invokes these fixture steps around its actual CLI calls.
// Raw SDK cleanup is independent of the s3-rm wrapper under test.
func TestR2ExampleFixture(t *testing.T) {
	bucket := os.Getenv("LIBAWS_R2_TEST_BUCKET")
	if bucket == "" {
		t.Skip("run through examples/misc/r2")
	}
	account := os.Getenv("LIBAWS_R2_TEST_ACCOUNT")
	if !r2AccountIDPattern.MatchString(account) || account != os.Getenv("R2_ACCOUNT_ID") || !regexp.MustCompile(`^libaws-testing-[0-9a-f]{32}$`).MatchString(bucket) {
		t.Fatal("R2 fixture requires an explicitly authorized account and unique testing bucket")
	}
	client, err := newR2S3ClientFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	switch step := os.Getenv("LIBAWS_R2_TEST_STEP"); step {
	case "absent":
		_, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(bucket), MaxKeys: aws.Int32(1)})
		if !isS3NoSuchBucket(err) {
			t.Fatalf("R2 fixture exists or absence could not be verified: %v", err)
		}
	case "create":
		if _, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)}); err != nil {
			t.Fatal(err)
		}
	case "cleanup":
		pages := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{Bucket: aws.String(bucket)})
		for pages.HasMorePages() {
			page, err := pages.NextPage(ctx)
			if isS3NoSuchBucket(err) {
				return
			}
			if err != nil {
				t.Error(err)
				break
			}
			for _, object := range page.Contents {
				if _, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: object.Key}); err != nil {
					t.Error(err)
				}
			}
		}
		if _, err := client.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(bucket)}); err != nil && !isS3NoSuchBucket(err) {
			t.Error(err)
		}
	default:
		t.Fatalf("unknown R2 fixture step %q", step)
	}
}

func TestNewR2S3ClientUsesOnlyExplicitR2Identity(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "aws-access-must-not-be-used")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "aws-secret-must-not-be-used")
	t.Setenv("AWS_REGION", "us-west-2")
	t.Setenv("R2_ACCOUNT_ID", "0123456789abcdef0123456789abcdef")
	t.Setenv("R2_ACCESS_KEY_ID", "r2-access")
	t.Setenv("R2_ACCESS_KEY_SECRET", "r2-secret")

	client, err := newR2S3ClientFromEnvironment()
	if err != nil {
		t.Fatalf("new R2 client: %v", err)
	}
	options := client.Options()
	if options.Region != r2Region {
		t.Fatalf("R2 region=%q, want %q", options.Region, r2Region)
	}
	wantEndpoint := "https://0123456789abcdef0123456789abcdef.r2.cloudflarestorage.com"
	if options.BaseEndpoint == nil || *options.BaseEndpoint != wantEndpoint {
		t.Fatalf("R2 endpoint=%v, want %q", options.BaseEndpoint, wantEndpoint)
	}
	if !options.UsePathStyle {
		t.Fatal("R2 client must use path-style bucket addressing")
	}
	identity, err := options.Credentials.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("retrieve R2 credentials: %v", err)
	}
	if identity.AccessKeyID != "r2-access" || identity.SecretAccessKey != "r2-secret" {
		t.Fatalf("R2 client used the wrong credential identity: access=%q", identity.AccessKeyID)
	}
}

func TestNewR2S3ClientRequiresCompleteValidatedEnvironment(t *testing.T) {
	valid := map[string]string{
		"R2_ACCOUNT_ID":        "0123456789abcdef0123456789abcdef",
		"R2_ACCESS_KEY_ID":     "r2-access",
		"R2_ACCESS_KEY_SECRET": "r2-secret",
	}
	for _, missing := range []string{"R2_ACCOUNT_ID", "R2_ACCESS_KEY_ID", "R2_ACCESS_KEY_SECRET"} {
		for name, value := range valid {
			t.Setenv(name, value)
		}
		t.Setenv(missing, "")
		_, err := newR2S3ClientFromEnvironment()
		if err == nil || !strings.Contains(err.Error(), missing) {
			t.Fatalf("missing %s error=%v", missing, err)
		}
	}

	t.Setenv("R2_ACCOUNT_ID", "not-an-account.example.com")
	t.Setenv("R2_ACCESS_KEY_ID", valid["R2_ACCESS_KEY_ID"])
	t.Setenv("R2_ACCESS_KEY_SECRET", valid["R2_ACCESS_KEY_SECRET"])
	_, err := newR2S3ClientFromEnvironment()
	if err == nil || !strings.Contains(err.Error(), "R2_ACCOUNT_ID") {
		t.Fatalf("invalid R2 account error=%v", err)
	}
}

func resetS3ClientStateForR2Test() {
	s3ClientLock.Lock()
	defer s3ClientLock.Unlock()
	s3Client = nil
	s3ClientsRegional = map[string]*s3.Client{}
	s3BucketClientOverride = nil
}

func TestS3UseR2RoutesAccountAndBucketOperationsToOneClient(t *testing.T) {
	resetS3ClientStateForR2Test()
	t.Cleanup(resetS3ClientStateForR2Test)
	t.Setenv("R2_ACCOUNT_ID", "0123456789abcdef0123456789abcdef")
	t.Setenv("R2_ACCESS_KEY_ID", "r2-access")
	t.Setenv("R2_ACCESS_KEY_SECRET", "r2-secret")

	if err := S3UseR2FromEnvironment(); err != nil {
		t.Fatalf("select R2: %v", err)
	}
	accountClient := S3Client()
	bucketClient, err := S3ClientBucketRegion(context.Background(), "bucket-without-aws-region-lookup")
	if err != nil {
		t.Fatalf("select R2 bucket client: %v", err)
	}
	if bucketClient != accountClient {
		t.Fatal("R2 account and bucket operations must use the same explicit client")
	}
	if err := S3UseR2FromEnvironment(); err == nil || !strings.Contains(err.Error(), "before initializing") {
		t.Fatalf("second R2 selection error=%v", err)
	}
}
