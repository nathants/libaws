package lib

import (
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func TestS3EncryptionConfigMatchesProviderSSECBlockingState(t *testing.T) {
	providerState := &s3types.ServerSideEncryptionConfiguration{
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
	if !reflect.DeepEqual(providerState, s3EncryptionConfig) {
		t.Fatalf("provider-normalized S3 encryption state would drift:\n got: %#v\nwant: %#v", providerState, s3EncryptionConfig)
	}
}
