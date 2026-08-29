package lib

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func matchingS3EncryptionTestConfig() *s3types.ServerSideEncryptionConfiguration {
	return &s3types.ServerSideEncryptionConfiguration{
		Rules: []s3types.ServerSideEncryptionRule{{
			ApplyServerSideEncryptionByDefault: &s3types.ServerSideEncryptionByDefault{
				SSEAlgorithm: s3types.ServerSideEncryptionAes256,
			},
			BlockedEncryptionTypes: &s3types.BlockedEncryptionTypes{
				EncryptionType: []s3types.EncryptionType{s3types.EncryptionTypeSseC},
			},
			BucketKeyEnabled: aws.Bool(false),
		}},
	}
}

func TestS3EncryptionMatchesPolicy(t *testing.T) {
	providerOmittedFalseBucketKey := matchingS3EncryptionTestConfig()
	providerOmittedFalseBucketKey.Rules[0].BucketKeyEnabled = nil

	missingRule := matchingS3EncryptionTestConfig()
	missingRule.Rules = nil

	extraRule := matchingS3EncryptionTestConfig()
	extraRule.Rules = append(extraRule.Rules, s3types.ServerSideEncryptionRule{})

	missingDefault := matchingS3EncryptionTestConfig()
	missingDefault.Rules[0].ApplyServerSideEncryptionByDefault = nil

	kmsAlgorithm := matchingS3EncryptionTestConfig()
	kmsAlgorithm.Rules[0].ApplyServerSideEncryptionByDefault.SSEAlgorithm = s3types.ServerSideEncryptionAwsKms

	unexpectedKMSKey := matchingS3EncryptionTestConfig()
	unexpectedKMSKey.Rules[0].ApplyServerSideEncryptionByDefault.KMSMasterKeyID = aws.String("key")

	missingSSECBlock := matchingS3EncryptionTestConfig()
	missingSSECBlock.Rules[0].BlockedEncryptionTypes = nil

	ssecAllowed := matchingS3EncryptionTestConfig()
	ssecAllowed.Rules[0].BlockedEncryptionTypes.EncryptionType = []s3types.EncryptionType{s3types.EncryptionTypeNone}

	multipleBlockedTypes := matchingS3EncryptionTestConfig()
	multipleBlockedTypes.Rules[0].BlockedEncryptionTypes.EncryptionType = []s3types.EncryptionType{
		s3types.EncryptionTypeSseC,
		s3types.EncryptionTypeNone,
	}

	bucketKeyEnabled := matchingS3EncryptionTestConfig()
	bucketKeyEnabled.Rules[0].BucketKeyEnabled = aws.Bool(true)

	tests := []struct {
		name   string
		config *s3types.ServerSideEncryptionConfiguration
		want   bool
	}{
		{name: "desired configuration", config: s3EncryptionConfig, want: true},
		{name: "provider omits false bucket key", config: providerOmittedFalseBucketKey, want: true},
		{name: "nil configuration", config: nil, want: false},
		{name: "missing rule", config: missingRule, want: false},
		{name: "extra rule", config: extraRule, want: false},
		{name: "missing default", config: missingDefault, want: false},
		{name: "KMS algorithm", config: kmsAlgorithm, want: false},
		{name: "unexpected KMS key", config: unexpectedKMSKey, want: false},
		{name: "missing SSE-C block", config: missingSSECBlock, want: false},
		{name: "SSE-C allowed", config: ssecAllowed, want: false},
		{name: "multiple blocked types", config: multipleBlockedTypes, want: false},
		{name: "bucket key enabled", config: bucketKeyEnabled, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := s3EncryptionMatchesPolicy(test.config); got != test.want {
				t.Fatalf("s3EncryptionMatchesPolicy() = %t, want %t for %#v", got, test.want, test.config)
			}
		})
	}
}

func TestValidateS3EncryptionPolicyIdentifiesUnsupportedBucketState(t *testing.T) {
	if err := validateS3EncryptionPolicy("managed-bucket", matchingS3EncryptionTestConfig()); err != nil {
		t.Fatalf("matching policy rejected: %v", err)
	}

	unsupported := matchingS3EncryptionTestConfig()
	unsupported.Rules[0].BlockedEncryptionTypes.EncryptionType = []s3types.EncryptionType{s3types.EncryptionTypeNone}
	err := validateS3EncryptionPolicy("unblocked-bucket", unsupported)
	if err == nil {
		t.Fatal("unsupported encryption policy was accepted")
	}
	if !strings.Contains(err.Error(), "unsupported encryption configuration") ||
		!strings.Contains(err.Error(), "unblocked-bucket") {
		t.Fatalf("validation error is not actionable: %v", err)
	}
}
