package lib

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

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
