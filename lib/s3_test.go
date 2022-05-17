package lib

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/gofrs/uuid"
)

func checkAccountS3() {
	account, err := StsAccount(context.Background())
	if err != nil {
		panic(err)
	}
	if os.Getenv("LIBAWS_TEST_ACCOUNT") != account {
		panic(fmt.Sprintf("%s != %s", os.Getenv("LIBAWS_TEST_ACCOUNT"), account))
	}
}

func TestS3Ensure(t *testing.T) {
	checkAccountS3()
	bucket := "libaws-s3-test-" + uuid.Must(uuid.NewV4()).String()
	input, err := S3EnsureInput("", bucket, []string{})
	if err != nil {
		t.Error(err)
		return
	}
	ctx := context.Background()
	err = S3Ensure(ctx, input, false)
	if err != nil {
		t.Error(err)
		return
	}
	defer func() {
		err := S3DeleteBucket(ctx, bucket, false)
		if err != nil {
			panic(err)
		}
	}()
}

func TestS3EnsureVersioningOffByDefault(t *testing.T) {
	checkAccountS3()
	bucket := "libaws-s3-test-" + uuid.Must(uuid.NewV4()).String()
	input, err := S3EnsureInput("", bucket, []string{})
	if err != nil {
		t.Error(err)
		return
	}
	ctx := context.Background()
	err = S3Ensure(ctx, input, false)
	if err != nil {
		t.Error(err)
		return
	}
	defer func() {
		err := S3DeleteBucket(ctx, bucket, false)
		if err != nil {
			panic(err)
		}
	}()
	out, err := S3Client().GetBucketVersioningWithContext(ctx, &s3.GetBucketVersioningInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		t.Error(err)
		return
	}
	if out.Status != nil {
		t.Error("versioning enabled")
		return
	}
}

func TestS3EnsureVersioning(t *testing.T) {
	checkAccountS3()
	bucket := "libaws-s3-test-" + uuid.Must(uuid.NewV4()).String()
	input, err := S3EnsureInput("", bucket, []string{"versioning=true"})
	if err != nil {
		t.Error(err)
		return
	}
	ctx := context.Background()
	err = S3Ensure(ctx, input, false)
	if err != nil {
		t.Error(err)
		return
	}
	defer func() {
		err := S3DeleteBucket(ctx, bucket, false)
		if err != nil {
			panic(err)
		}
	}()
	out, err := S3Client().GetBucketVersioningWithContext(ctx, &s3.GetBucketVersioningInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		t.Error(err)
		return
	}
	if *out.Status != s3.BucketVersioningStatusEnabled {
		t.Error("versioning not enabled")
		return
	}
}

func TestS3EnsureUpdateVersioning(t *testing.T) {
	checkAccountS3()
	bucket := "libaws-s3-test-" + uuid.Must(uuid.NewV4()).String()
	ctx := context.Background()
	input, err := S3EnsureInput("", bucket, []string{})
	if err != nil {
		t.Error(err)
		return
	}
	err = S3Ensure(ctx, input, false)
	if err != nil {
		t.Error(err)
		return
	}
	defer func() {
		err := S3DeleteBucket(ctx, bucket, false)
		if err != nil {
			panic(err)
		}
	}()
	out, err := S3Client().GetBucketVersioningWithContext(ctx, &s3.GetBucketVersioningInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		t.Error(err)
		return
	}
	if out.Status != nil {
		t.Error("versioning enabled")
		return
	}
	input, err = S3EnsureInput("", bucket, []string{"versioning=true"})
	if err != nil {
		t.Error(err)
		return
	}
	err = S3Ensure(ctx, input, false)
	if err != nil {
		t.Error(err)
		return
	}
	out, err = S3Client().GetBucketVersioningWithContext(ctx, &s3.GetBucketVersioningInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		t.Error(err)
		return
	}
	if *out.Status != s3.BucketVersioningStatusEnabled {
		t.Error("versioning not enabled")
		return
	}
	input, err = S3EnsureInput("", bucket, []string{"versioning=false"})
	if err != nil {
		t.Error(err)
		return
	}
	err = S3Ensure(ctx, input, false)
	if err != nil {
		t.Error(err)
		return
	}
	out, err = S3Client().GetBucketVersioningWithContext(ctx, &s3.GetBucketVersioningInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		t.Error(err)
		return
	}
	if *out.Status != s3.BucketVersioningStatusSuspended {
		t.Error("versioning enable enabled")
		return
	}
}

func TestS3EnsureEncryptionOnByDefault(t *testing.T) {
	checkAccountS3()
	bucket := "libaws-s3-test-" + uuid.Must(uuid.NewV4()).String()
	input, err := S3EnsureInput("", bucket, []string{})
	if err != nil {
		t.Error(err)
		return
	}
	ctx := context.Background()
	err = S3Ensure(ctx, input, false)
	if err != nil {
		t.Error(err)
		return
	}
	defer func() {
		err := S3DeleteBucket(ctx, bucket, false)
		if err != nil {
			panic(err)
		}
	}()
	out, err := S3Client().GetBucketEncryptionWithContext(ctx, &s3.GetBucketEncryptionInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		t.Error(err)
		return
	}
	encryptedConfig := &s3.ServerSideEncryptionConfiguration{
		Rules: []*s3.ServerSideEncryptionRule{{
			BucketKeyEnabled: aws.Bool(false),
			ApplyServerSideEncryptionByDefault: &s3.ServerSideEncryptionByDefault{
				SSEAlgorithm:   aws.String(s3.ServerSideEncryptionAes256),
				KMSMasterKeyID: nil,
			},
		}},
	}
	if !reflect.DeepEqual(out.ServerSideEncryptionConfiguration, encryptedConfig) {
		t.Error("encryption not enabled")
		return
	}
}

func TestS3EnsurePrivateByDefault(t *testing.T) {
	checkAccountS3()
	bucket := "libaws-s3-test-" + uuid.Must(uuid.NewV4()).String()
	input, err := S3EnsureInput("", bucket, []string{})
	if err != nil {
		t.Error(err)
		return
	}
	ctx := context.Background()
	err = S3Ensure(ctx, input, false)
	if err != nil {
		t.Error(err)
		return
	}
	defer func() {
		err := S3DeleteBucket(ctx, bucket, false)
		if err != nil {
			panic(err)
		}
	}()
	pabOut, err := S3Client().GetPublicAccessBlockWithContext(ctx, &s3.GetPublicAccessBlockInput{
		Bucket: aws.String(input.name),
	})
	if err != nil {
		t.Error(err)
		return
	}
	privateConf := &s3.PublicAccessBlockConfiguration{
		BlockPublicAcls:       aws.Bool(true),
		IgnorePublicAcls:      aws.Bool(true),
		BlockPublicPolicy:     aws.Bool(true),
		RestrictPublicBuckets: aws.Bool(true),
	}
	if !reflect.DeepEqual(pabOut.PublicAccessBlockConfiguration, privateConf) {
		t.Error("not private")
	}
	_, err = S3Client().GetBucketPolicyWithContext(ctx, &s3.GetBucketPolicyInput{
		Bucket: aws.String(bucket),
	})
	if err == nil {
		t.Error("bucket policy should not exist")
		return
	}
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != "NoSuchBucketPolicy" {
			t.Error(err)
			return
		}
	}
}

func TestS3EnsurePrivateCors(t *testing.T) {
	checkAccountS3()
	bucket := "libaws-s3-test-" + uuid.Must(uuid.NewV4()).String()
	input, err := S3EnsureInput("", bucket, []string{"acl=private", "cors=true"})
	if err != nil {
		t.Error(err)
		return
	}
	ctx := context.Background()
	err = S3Ensure(ctx, input, false)
	if err != nil {
		t.Error(err)
		return
	}
	defer func() {
		err := S3DeleteBucket(ctx, bucket, false)
		if err != nil {
			panic(err)
		}
	}()
	cors, err := S3Client().GetBucketCorsWithContext(ctx, &s3.GetBucketCorsInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		t.Error(err)
		return
	}
	if !reflect.DeepEqual(cors.CORSRules, s3Cors) {
		t.Error("cors config misconfigured")
		return
	}
	pabOut, err := S3Client().GetPublicAccessBlockWithContext(ctx, &s3.GetPublicAccessBlockInput{
		Bucket: aws.String(input.name),
	})
	if err != nil {
		t.Error(err)
		return
	}
	privateConf := &s3.PublicAccessBlockConfiguration{
		BlockPublicAcls:       aws.Bool(true),
		IgnorePublicAcls:      aws.Bool(true),
		BlockPublicPolicy:     aws.Bool(true),
		RestrictPublicBuckets: aws.Bool(true),
	}
	if !reflect.DeepEqual(pabOut.PublicAccessBlockConfiguration, privateConf) {
		t.Error("not private")
	}
	_, err = S3Client().GetBucketPolicyWithContext(ctx, &s3.GetBucketPolicyInput{
		Bucket: aws.String(bucket),
	})
	if err == nil {
		t.Error("bucket policy should not exist")
		return
	}
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != "NoSuchBucketPolicy" {
			t.Error(err)
			return
		}
	}
}

func TestS3EnsurePublic(t *testing.T) {
	checkAccountS3()
	bucket := "libaws-s3-test-" + uuid.Must(uuid.NewV4()).String()
	input, err := S3EnsureInput("", bucket, []string{"acl=public"})
	if err != nil {
		t.Error(err)
		return
	}
	ctx := context.Background()
	err = S3Ensure(ctx, input, false)
	if err != nil {
		t.Error(err)
		return
	}
	defer func() {
		err := S3DeleteBucket(ctx, bucket, false)
		if err != nil {
			panic(err)
		}
	}()
	_, err = S3Client().GetBucketCorsWithContext(ctx, &s3.GetBucketCorsInput{
		Bucket: aws.String(bucket),
	})
	if err == nil {
		t.Error("cors config misconfigured")
		return
	}
	policyOut, err := S3Client().GetBucketPolicyWithContext(ctx, &s3.GetBucketPolicyInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		t.Error(err)
		return
	}
	policy := IamPolicyDocument{}
	err = json.Unmarshal([]byte(*policyOut.Policy), &policy)
	if err != nil {
		t.Error(err)
		return
	}
	if !reflect.DeepEqual(policy, s3PublicPolicy(bucket)) {
		t.Error("cors config misconfigured")
		return
	}
}

func TestS3EnsurePublicCors(t *testing.T) {
	checkAccountS3()
	bucket := "libaws-s3-test-" + uuid.Must(uuid.NewV4()).String()
	input, err := S3EnsureInput("", bucket, []string{"acl=public", "cors=true"})
	if err != nil {
		t.Error(err)
		return
	}
	ctx := context.Background()
	err = S3Ensure(ctx, input, false)
	if err != nil {
		t.Error(err)
		return
	}
	defer func() {
		err := S3DeleteBucket(ctx, bucket, false)
		if err != nil {
			panic(err)
		}
	}()
	cors, err := S3Client().GetBucketCorsWithContext(ctx, &s3.GetBucketCorsInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		t.Error(err)
		return
	}
	if !reflect.DeepEqual(cors.CORSRules, s3Cors) {
		t.Error("cors config misconfigured")
		return
	}
	policyOut, err := S3Client().GetBucketPolicyWithContext(ctx, &s3.GetBucketPolicyInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		t.Error(err)
		return
	}
	policy := IamPolicyDocument{}
	err = json.Unmarshal([]byte(*policyOut.Policy), &policy)
	if err != nil {
		t.Error(err)
		return
	}
	if !reflect.DeepEqual(policy, s3PublicPolicy(bucket)) {
		t.Error("cors config misconfigured")
		return
	}
}

func TestS3EnsurePrivateToPublicNotAllowed(t *testing.T) {
	checkAccountS3()
	bucket := "libaws-s3-test-" + uuid.Must(uuid.NewV4()).String()
	ctx := context.Background()
	input, err := S3EnsureInput("", bucket, []string{"acl=private"})
	if err != nil {
		t.Error(err)
		return
	}
	err = S3Ensure(ctx, input, false)
	if err != nil {
		t.Error(err)
		return
	}
	defer func() {
		err := S3DeleteBucket(ctx, bucket, false)
		if err != nil {
			panic(err)
		}
	}()
	input, err = S3EnsureInput("", bucket, []string{"acl=public"})
	if err != nil {
		t.Error(err)
		return
	}
	err = S3Ensure(ctx, input, false)
	if err == nil {
		t.Error("expected error")
		return
	}
}

func TestS3EnsurePublicToPrivateNotAllowed(t *testing.T) {
	checkAccountS3()
	bucket := "libaws-s3-test-" + uuid.Must(uuid.NewV4()).String()
	ctx := context.Background()
	input, err := S3EnsureInput("", bucket, []string{"acl=public"})
	if err != nil {
		t.Error(err)
		return
	}
	err = S3Ensure(ctx, input, false)
	if err != nil {
		t.Error(err)
		return
	}
	defer func() {
		err := S3DeleteBucket(ctx, bucket, false)
		if err != nil {
			panic(err)
		}
	}()
	input, err = S3EnsureInput("", bucket, []string{"acl=private"})
	if err != nil {
		t.Error(err)
		return
	}
	err = S3Ensure(ctx, input, false)
	if err == nil {
		t.Error("expected error")
		return
	}
}
