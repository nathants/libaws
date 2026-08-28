package lib

import (
	"context"
	"fmt"
	"os"
	"regexp"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const r2Region = "auto"

var r2AccountIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var s3BucketClientOverride *s3.Client

func requiredR2Environment(name string) (string, error) {
	value := os.Getenv(name)
	if value == "" {
		return "", fmt.Errorf("%s must be set for explicit R2 mode", name)
	}
	return value, nil
}

func newR2S3ClientFromEnvironment() (*s3.Client, error) {
	accountID, err := requiredR2Environment("R2_ACCOUNT_ID")
	if err != nil {
		return nil, err
	}
	if !r2AccountIDPattern.MatchString(accountID) {
		return nil, fmt.Errorf("R2_ACCOUNT_ID must be exactly 32 lowercase hexadecimal characters")
	}
	accessKeyID, err := requiredR2Environment("R2_ACCESS_KEY_ID")
	if err != nil {
		return nil, err
	}
	accessKeySecret, err := requiredR2Environment("R2_ACCESS_KEY_SECRET")
	if err != nil {
		return nil, err
	}

	cfg, err := config.LoadDefaultConfig(
		context.Background(),
		config.WithRegion(r2Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKeyID, accessKeySecret, "")),
		config.WithRetryMaxAttempts(5),
	)
	if err != nil {
		return nil, fmt.Errorf("load explicit R2 client configuration: %w", err)
	}
	endpoint := "https://" + accountID + ".r2.cloudflarestorage.com"
	return s3.NewFromConfig(cfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(endpoint)
		options.UsePathStyle = true
	}), nil
}

// S3UseR2FromEnvironment explicitly selects Cloudflare R2 for this process.
// It must be called before any ordinary AWS S3 client is initialized.
func S3UseR2FromEnvironment() error {
	client, err := newR2S3ClientFromEnvironment()
	if err != nil {
		return err
	}

	s3ClientLock.Lock()
	defer s3ClientLock.Unlock()
	if s3Client != nil || len(s3ClientsRegional) != 0 || s3BucketClientOverride != nil {
		return fmt.Errorf("explicit R2 mode must be selected before initializing an S3 client")
	}
	s3Client = client
	s3BucketClientOverride = client
	return nil
}

func configuredS3BucketClientOverride() *s3.Client {
	s3ClientLock.Lock()
	defer s3ClientLock.Unlock()
	return s3BucketClientOverride
}
