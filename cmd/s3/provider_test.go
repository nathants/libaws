package libaws

import (
	"strings"
	"testing"
)

type s3CommandDescriber interface {
	Description() string
}

func TestS3CommandProviderSupportIsExplicit(t *testing.T) {
	tests := []struct {
		name     string
		args     s3CommandDescriber
		supports string
	}{
		{name: "describe", args: s3DescribeArgs{}, supports: "provider: AWS S3 only"},
		{name: "ensure", args: s3EnsureArgs{}, supports: "provider: AWS S3 only"},
		{name: "get", args: s3GetArgs{}, supports: "providers: AWS S3 (default), Cloudflare R2 (--r2)"},
		{name: "get-version", args: s3GetVersionArgs{}, supports: "provider: AWS S3 only"},
		{name: "head", args: s3HeadArgs{}, supports: "providers: AWS S3 (default), Cloudflare R2 (--r2)"},
		{name: "ls", args: s3LsArgs{}, supports: "providers: AWS S3 (default), Cloudflare R2 (--r2)"},
		{name: "ls-versions", args: s3LsVersionsArgs{}, supports: "provider: AWS S3 only"},
		{name: "presign-get", args: s3PresignGetArgs{}, supports: "provider: AWS S3 only"},
		{name: "presign-put", args: s3PresignPutArgs{}, supports: "provider: AWS S3 only"},
		{name: "put", args: s3PutArgs{}, supports: "providers: AWS S3 (default), Cloudflare R2 (--r2)"},
		{name: "rm", args: s3RmArgs{}, supports: "providers: AWS S3 (default), Cloudflare R2 (--r2)"},
		{name: "rm-bucket", args: s3RmBucketArgs{}, supports: "provider: AWS S3 only"},
		{name: "rm-versions", args: s3RmVersionsArgs{}, supports: "provider: AWS S3 only"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			description := test.args.Description()
			if !strings.Contains(description, test.supports) {
				t.Fatalf("description %q does not state %q", description, test.supports)
			}
		})
	}
}
