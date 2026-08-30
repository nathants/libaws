package lib

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/gofrs/uuid"
)

func checkS3BucketPolicy(t *testing.T, ctx context.Context, input *s3EnsureInput) {
	t.Helper()
	out, err := S3Client().GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{Bucket: aws.String(input.name)})
	if err != nil {
		t.Fatal(err)
	}
	expected, err := s3DesiredPolicy(input)
	if err != nil {
		t.Fatal(err)
	}
	equal, err := iamPolicyEqual(aws.ToString(out.Policy), expected)
	if err != nil {
		t.Fatal(err)
	}
	if !equal {
		t.Fatalf("bucket policy mismatch:\ngot:  %s\nwant: %s", aws.ToString(out.Policy), expected)
	}
}

func TestS3Ensure(t *testing.T) {
	requireLiveAWSAccount(t)
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
	requireLiveAWSAccount(t)
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
	out, err := S3Client().GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		t.Error(err)
		return
	}
	if out.Status == s3types.BucketVersioningStatusEnabled {
		t.Error("versioning enabled")
		return
	}
}

func TestS3EnsureVersioning(t *testing.T) {
	requireLiveAWSAccount(t)
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
	out, err := S3Client().GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		t.Error(err)
		return
	}
	if out.Status == s3types.BucketVersioningStatusSuspended {
		t.Error("versioning not enabled")
		return
	}
}

func TestS3EnsureUpdateVersioning(t *testing.T) {
	requireLiveAWSAccount(t)
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
	out, err := S3Client().GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		t.Error(err)
		return
	}
	if out.Status == s3types.BucketVersioningStatusEnabled {
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
	out, err = S3Client().GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		t.Error(err)
		return
	}
	if out.Status == s3types.BucketVersioningStatusSuspended {
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
	out, err = S3Client().GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		t.Error(err)
		return
	}
	if out.Status == s3types.BucketVersioningStatusEnabled {
		t.Error("versioning enable enabled")
		return
	}
}

func TestS3EnsureEncryptionOnByDefault(t *testing.T) {
	requireLiveAWSAccount(t)
	bucket := "libaws-s3-test-" + uuid.Must(uuid.NewV4()).String()
	input, err := S3EnsureInput("", bucket, []string{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := S3Ensure(ctx, input, false); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := S3DeleteBucket(ctx, bucket, false); err != nil {
			panic(err)
		}
	}()
	client := S3Client()
	getEncryption := func() *s3types.ServerSideEncryptionConfiguration {
		t.Helper()
		out, err := client.GetBucketEncryption(ctx, &s3.GetBucketEncryptionInput{
			Bucket: aws.String(bucket),
		})
		if err != nil {
			t.Fatal(err)
		}
		return out.ServerSideEncryptionConfiguration
	}
	capturePreview := func() (string, error) {
		var logs strings.Builder
		originalPrint := Logger.Print
		Logger.Print = func(args ...any) {
			_, _ = fmt.Fprint(&logs, args...)
		}
		defer func() {
			Logger.Print = originalPrint
		}()
		err := S3Ensure(ctx, input, true)
		return logs.String(), err
	}
	listS3 := func() (map[string]*InfraS3, error) {
		triggers := make(chan *InfraTrigger)
		drained := make(chan struct{})
		go func() {
			defer close(drained)
			for range triggers {
			}
		}()
		buckets, err := InfraListS3(ctx, triggers)
		close(triggers)
		<-drained
		return buckets, err
	}

	config := getEncryption()
	if err := validateS3EncryptionPolicy(bucket, config); err != nil {
		t.Fatalf("new bucket encryption does not match policy: %v", err)
	}

	_, err = client.PutBucketEncryption(ctx, &s3.PutBucketEncryptionInput{
		Bucket: aws.String(bucket),
		ServerSideEncryptionConfiguration: &s3types.ServerSideEncryptionConfiguration{
			Rules: []s3types.ServerSideEncryptionRule{{
				ApplyServerSideEncryptionByDefault: &s3types.ServerSideEncryptionByDefault{
					SSEAlgorithm: s3types.ServerSideEncryptionAes256,
				},
				BlockedEncryptionTypes: &s3types.BlockedEncryptionTypes{
					EncryptionType: []s3types.EncryptionType{s3types.EncryptionTypeNone},
				},
				BucketKeyEnabled: aws.Bool(false),
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	config = getEncryption()
	err = validateS3EncryptionPolicy(bucket, config)
	if err == nil {
		t.Fatalf("test drift did not allow SSE-C: %s", PformatAlways(config))
	}
	if !strings.Contains(err.Error(), bucket) {
		t.Fatalf("unsupported live state error does not identify bucket: %v", err)
	}
	_, err = listS3()
	if err == nil {
		t.Fatal("infrastructure inspection accepted an unsupported encryption configuration")
	}
	if !strings.Contains(err.Error(), bucket) || !strings.Contains(err.Error(), "unsupported encryption configuration") {
		t.Fatalf("infrastructure inspection returned the wrong encryption error: %v", err)
	}
	logs, err := capturePreview()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs, "preview: updated encryption for "+bucket) {
		t.Fatalf("preview did not report encryption drift: %s", logs)
	}

	if err := S3Ensure(ctx, input, false); err != nil {
		t.Fatal(err)
	}
	config = getEncryption()
	if err := validateS3EncryptionPolicy(bucket, config); err != nil {
		t.Fatalf("S3Ensure did not restore encryption policy: %v", err)
	}
	logs, err = capturePreview()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs, "encryption for "+bucket) {
		t.Fatalf("converged bucket still reports encryption drift: %s", logs)
	}
	_, err = client.DeleteBucketTagging(ctx, &s3.DeleteBucketTaggingInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		t.Fatal(err)
	}
	infraBuckets, err := listS3()
	if err != nil {
		t.Fatalf("inspect converged infrastructure: %v", err)
	}
	infraBucket, exists := infraBuckets[bucket]
	if !exists {
		t.Fatalf("infrastructure inspection omitted managed bucket %q", bucket)
	}
	for _, attr := range infraBucket.Attr {
		if strings.HasPrefix(attr, "encryption=") {
			t.Fatalf("infrastructure inspection emitted removed encryption attribute %q", attr)
		}
	}
}

func TestS3EnsurePrivateByDefault(t *testing.T) {
	requireLiveAWSAccount(t)
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
	pabOut, err := S3Client().GetPublicAccessBlock(ctx, &s3.GetPublicAccessBlockInput{
		Bucket: aws.String(input.name),
	})
	if err != nil {
		t.Error(err)
		return
	}
	privateConf := &s3types.PublicAccessBlockConfiguration{
		BlockPublicAcls:       aws.Bool(true),
		IgnorePublicAcls:      aws.Bool(true),
		BlockPublicPolicy:     aws.Bool(true),
		RestrictPublicBuckets: aws.Bool(true),
	}
	if !reflect.DeepEqual(pabOut.PublicAccessBlockConfiguration, privateConf) {
		t.Error("not private")
	}
	checkS3BucketPolicy(t, ctx, input)
}

func TestS3EnsurePrivateCors(t *testing.T) {
	requireLiveAWSAccount(t)
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
	cors, err := S3Client().GetBucketCors(ctx, &s3.GetBucketCorsInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		t.Error(err)
		return
	}
	if !reflect.DeepEqual(cors.CORSRules, s3Cors(nil)) {
		t.Error("cors config misconfigured")
		return
	}
	pabOut, err := S3Client().GetPublicAccessBlock(ctx, &s3.GetPublicAccessBlockInput{
		Bucket: aws.String(input.name),
	})
	if err != nil {
		t.Error(err)
		return
	}
	privateConf := &s3types.PublicAccessBlockConfiguration{
		BlockPublicAcls:       aws.Bool(true),
		IgnorePublicAcls:      aws.Bool(true),
		BlockPublicPolicy:     aws.Bool(true),
		RestrictPublicBuckets: aws.Bool(true),
	}
	if !reflect.DeepEqual(pabOut.PublicAccessBlockConfiguration, privateConf) {
		t.Error("not private")
	}
	checkS3BucketPolicy(t, ctx, input)
}

func TestS3EnsurePublic(t *testing.T) {
	requireLiveAWSAccount(t)
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
	_, err = S3Client().GetBucketCors(ctx, &s3.GetBucketCorsInput{
		Bucket: aws.String(bucket),
	})
	if err == nil {
		t.Error("cors config misconfigured")
		return
	}
	checkS3BucketPolicy(t, ctx, input)
}

func TestS3EnsurePublicCors(t *testing.T) {
	requireLiveAWSAccount(t)
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
	cors, err := S3Client().GetBucketCors(ctx, &s3.GetBucketCorsInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		t.Error(err)
		return
	}
	if !reflect.DeepEqual(cors.CORSRules, s3Cors(nil)) {
		t.Error("cors config misconfigured")
		return
	}
	checkS3BucketPolicy(t, ctx, input)
}

func TestS3EnsurePrivateToPublicNotAllowed(t *testing.T) {
	requireLiveAWSAccount(t)
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
	requireLiveAWSAccount(t)
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
