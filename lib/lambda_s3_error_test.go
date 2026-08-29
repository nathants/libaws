package lib

import (
	"fmt"
	"testing"

	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type codedS3Error struct {
	code string
}

func (err codedS3Error) Error() string {
	return "S3 API error: " + err.code
}

func (err codedS3Error) ErrorCode() string {
	return err.code
}

func TestIsS3NoSuchBucketRecognizesWrappedGenericAPIError(t *testing.T) {
	err := fmt.Errorf("operation error S3: %w", codedS3Error{code: s3ErrCodeNoSuchBucket})
	if !isS3NoSuchBucket(err) {
		t.Fatalf("wrapped generic %s was not recognized: %v", s3ErrCodeNoSuchBucket, err)
	}
	if !isS3NoSuchBucket(&s3types.NoSuchBucket{}) {
		t.Fatal("typed NoSuchBucket was not recognized")
	}
}

func TestIsS3NoSuchBucketRejectsOtherErrors(t *testing.T) {
	if isS3NoSuchBucket(codedS3Error{code: s3ErrCodeNoSuchBucketPolicy}) {
		t.Fatal("NoSuchBucketPolicy was classified as NoSuchBucket")
	}
	if isS3NoSuchBucket(&s3types.NoSuchKey{}) {
		t.Fatal("typed NoSuchKey was classified as NoSuchBucket")
	}
}
