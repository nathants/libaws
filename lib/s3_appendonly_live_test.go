package lib

import (
	"context"
	"errors"
	"io"
	"net/url"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/gofrs/uuid"
)

func TestS3EnsureAppendOnlyConvergesAndEnforces(t *testing.T) {
	requireLiveAWSAccount(t)
	ctx := context.Background()
	requireErrorCode := func(operation string, err error, want string) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s succeeded", operation)
		}
		var apiErr interface{ ErrorCode() string }
		if !errors.As(err, &apiErr) {
			t.Fatalf("%s returned non-API error: %v", operation, err)
		}
		if apiErr.ErrorCode() != want {
			t.Fatalf("%s error code = %q, want %q: %v", operation, apiErr.ErrorCode(), want, err)
		}
	}
	bucket := "libaws-appendonly-test-" + uuid.Must(uuid.NewV4()).String()
	input, err := S3EnsureInput("appendonly-test", bucket, []string{
		"acl=private",
		"versioning=true",
		"appendonly=true",
		"allow_put=lambda.amazonaws.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := S3DeleteBucket(ctx, bucket, false); err != nil {
			t.Error(err)
		}
	}()
	if err := S3Ensure(ctx, input, false); err != nil {
		t.Fatal(err)
	}

	client, err := S3ClientBucketRegion(ctx, bucket)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := client.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{Bucket: aws.String(bucket)})
	if err != nil {
		t.Fatal(err)
	}
	if !s3AppendOnlyPolicyMatches(bucket, aws.ToString(policy.Policy)) {
		t.Fatalf("bucket does not have the append-only policy: %s", aws.ToString(policy.Policy))
	}
	versioning, err := client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{Bucket: aws.String(bucket)})
	if err != nil {
		t.Fatal(err)
	}
	if versioning.Status != s3types.BucketVersioningStatusEnabled {
		t.Fatalf("versioning status = %s", versioning.Status)
	}

	key := "retained-probe"
	put := &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        strings.NewReader("original"),
		IfNoneMatch: aws.String("*"),
	}
	created, err := client.PutObject(ctx, put)
	if err != nil {
		t.Fatalf("create-only PutObject without an explicit encryption header failed: %v", err)
	}
	head, err := client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		t.Fatal(err)
	}
	if head.ServerSideEncryption != s3types.ServerSideEncryptionAes256 {
		t.Fatalf("default object encryption = %q, want AES256", head.ServerSideEncryption)
	}
	put.Body = strings.NewReader("replacement")
	_, err = client.PutObject(ctx, put)
	requireErrorCode("conditional overwrite", err, "PreconditionFailed")

	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:               aws.String(bucket),
		Key:                  aws.String(key),
		Body:                 strings.NewReader("unconditional replacement"),
		ServerSideEncryption: s3types.ServerSideEncryptionAes256,
	})
	requireErrorCode("unconditional PutObject", err, "AccessDenied")

	multipartKey := "multipart-probe"
	var activeMultipartKey string
	var activeMultipartUploadID *string
	abortActiveMultipartUpload := func() {
		if activeMultipartUploadID == nil {
			return
		}
		_, abortErr := client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
			Bucket:   aws.String(bucket),
			Key:      aws.String(activeMultipartKey),
			UploadId: activeMultipartUploadID,
		})
		if abortErr != nil {
			var apiErr interface{ ErrorCode() string }
			if !errors.As(abortErr, &apiErr) || apiErr.ErrorCode() != "NoSuchUpload" {
				t.Errorf("abort multipart upload: %v", abortErr)
				return
			}
		}
		activeMultipartKey = ""
		activeMultipartUploadID = nil
	}
	defer abortActiveMultipartUpload()

	multipart, err := client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(multipartKey),
	})
	if err != nil {
		t.Fatalf("CreateMultipartUpload: %v", err)
	}
	activeMultipartKey = multipartKey
	activeMultipartUploadID = multipart.UploadId
	part, err := client.UploadPart(ctx, &s3.UploadPartInput{
		Bucket:     aws.String(bucket),
		Key:        aws.String(multipartKey),
		UploadId:   multipart.UploadId,
		PartNumber: aws.Int32(1),
		Body:       strings.NewReader("multipart-original"),
	})
	if err != nil {
		t.Fatalf("UploadPart: %v", err)
	}
	completedParts := &s3types.CompletedMultipartUpload{Parts: []s3types.CompletedPart{{
		ETag:       part.ETag,
		PartNumber: aws.Int32(1),
	}}}
	_, err = client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:          aws.String(bucket),
		Key:             aws.String(multipartKey),
		UploadId:        multipart.UploadId,
		MultipartUpload: completedParts,
	})
	requireErrorCode("unconditional CompleteMultipartUpload", err, "AccessDenied")
	_, err = client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:          aws.String(bucket),
		Key:             aws.String(multipartKey),
		UploadId:        multipart.UploadId,
		MultipartUpload: completedParts,
		IfNoneMatch:     aws.String("*"),
	})
	if err != nil {
		t.Fatalf("conditional CompleteMultipartUpload: %v", err)
	}
	activeMultipartKey = ""
	activeMultipartUploadID = nil

	overwriteMultipart, err := client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(multipartKey),
	})
	if err != nil {
		t.Fatalf("overwrite CreateMultipartUpload: %v", err)
	}
	activeMultipartKey = multipartKey
	activeMultipartUploadID = overwriteMultipart.UploadId
	overwritePart, err := client.UploadPart(ctx, &s3.UploadPartInput{
		Bucket:     aws.String(bucket),
		Key:        aws.String(multipartKey),
		UploadId:   overwriteMultipart.UploadId,
		PartNumber: aws.Int32(1),
		Body:       strings.NewReader("multipart-replacement"),
	})
	if err != nil {
		t.Fatalf("overwrite UploadPart: %v", err)
	}
	_, err = client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(multipartKey),
		UploadId: overwriteMultipart.UploadId,
		MultipartUpload: &s3types.CompletedMultipartUpload{Parts: []s3types.CompletedPart{{
			ETag:       overwritePart.ETag,
			PartNumber: aws.Int32(1),
		}}},
		IfNoneMatch: aws.String("*"),
	})
	requireErrorCode("conditional multipart overwrite", err, "PreconditionFailed")
	abortActiveMultipartUpload()

	multipartObject, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(multipartKey),
	})
	if err != nil {
		t.Fatal(err)
	}
	multipartData, multipartReadErr := io.ReadAll(multipartObject.Body)
	multipartCloseErr := multipartObject.Body.Close()
	if multipartReadErr != nil {
		t.Fatal(multipartReadErr)
	}
	if multipartCloseErr != nil {
		t.Fatal(multipartCloseErr)
	}
	if string(multipartData) != "multipart-original" {
		t.Fatalf("retained multipart object = %q, want original", multipartData)
	}

	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	requireErrorCode("DeleteObject", err, "AccessDenied")

	if aws.ToString(created.VersionId) == "" {
		t.Fatal("versioned PutObject did not return a version ID")
	}
	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket:    aws.String(bucket),
		Key:       aws.String(key),
		VersionId: created.VersionId,
	})
	requireErrorCode("DeleteObjectVersion", err, "AccessDenied")

	_, err = client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:               aws.String(bucket),
		Key:                  aws.String(key),
		CopySource:           aws.String(url.PathEscape(bucket + "/" + key)),
		ServerSideEncryption: s3types.ServerSideEncryptionAes256,
	})
	requireErrorCode("CopyObject", err, "AccessDenied")
	got, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(got.Body)
	closeErr := got.Body.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if string(data) != "original" {
		t.Fatalf("retained object = %q, want original", data)
	}

	drifted := strings.Replace(aws.ToString(policy.Policy), `"s3:if-none-match":"*"`, `"s3:if-none-match":"wrong"`, 1)
	if drifted == aws.ToString(policy.Policy) {
		t.Fatal("failed to construct drifted policy")
	}
	if _, err := client.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
		Bucket: aws.String(bucket),
		Policy: aws.String(drifted),
	}); err != nil {
		t.Fatal(err)
	}
	if err := S3Ensure(ctx, input, false); err != nil {
		t.Fatalf("converge drifted append-only policy: %v", err)
	}
	policy, err = client.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{Bucket: aws.String(bucket)})
	if err != nil {
		t.Fatal(err)
	}
	if !s3AppendOnlyPolicyMatches(bucket, aws.ToString(policy.Policy)) {
		t.Fatal("security-critical bucket-policy drift was not corrected")
	}
}
