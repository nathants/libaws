package cliaws

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["s3-describe"] = s3Describe
}

type s3DescribeArgs struct {
	Name string `arg:"positional,required"`
}

func (s3DescribeArgs) Description() string {
	return "\ndescribe a s3 bucket\n"
}

type s3BucketDescription struct {
	Versioning    bool
	Acl           *s3.GetBucketAclOutput
	Cors          []*s3.CORSRule
	Encryption    *s3.ServerSideEncryptionConfiguration
	Lifecycle     []*s3.LifecycleRule
	Region        string
	Logging       *s3.LoggingEnabled
	Notifications *s3.NotificationConfiguration
	Policy        *lib.IamPolicyDocument
	Replication   *s3.ReplicationConfiguration
}

func s3Describe() {
	var args s3DescribeArgs
	arg.MustParse(&args)
	ctx := context.Background()

	bucket := args.Name

	var descr s3BucketDescription

	s3Client, err := lib.S3ClientBucketRegion(bucket)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}

	version, err := s3Client.GetBucketVersioningWithContext(ctx, &s3.GetBucketVersioningInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	if version.Status != nil {
		descr.Versioning = *version.Status == s3.BucketVersioningStatusEnabled
	}

	acl, err := s3Client.GetBucketAclWithContext(ctx, &s3.GetBucketAclInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	descr.Acl = acl

	cors, err := s3Client.GetBucketCorsWithContext(ctx, &s3.GetBucketCorsInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != "NoSuchCORSConfiguration" {
			lib.Logger.Fatal("error: ", err)
		}
	} else {
		descr.Cors = cors.CORSRules
	}

	encryption, err := s3Client.GetBucketEncryptionWithContext(ctx, &s3.GetBucketEncryptionInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != "ServerSideEncryptionConfigurationNotFoundError" {
			lib.Logger.Fatal("error: ", err)
		}
	} else {
		descr.Encryption = encryption.ServerSideEncryptionConfiguration
	}

	lifecycle, err := s3Client.GetBucketLifecycleConfigurationWithContext(ctx, &s3.GetBucketLifecycleConfigurationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != "NoSuchLifecycleConfiguration" {
			lib.Logger.Fatal("error: ", err)
		}
	} else {
		descr.Lifecycle = lifecycle.Rules
	}

	region, err := lib.S3BucketRegion(bucket)
	if err != nil {
	    lib.Logger.Fatal("error: ", err)
	}
	descr.Region = region

	logging, err := s3Client.GetBucketLoggingWithContext(ctx, &s3.GetBucketLoggingInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	descr.Logging = logging.LoggingEnabled

	notif, err := s3Client.GetBucketNotificationConfigurationWithContext(ctx, &s3.GetBucketNotificationConfigurationRequest{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	if notif.LambdaFunctionConfigurations != nil || notif.QueueConfigurations != nil || notif.TopicConfigurations != nil {
		descr.Notifications = notif
	}

	policy, err := s3Client.GetBucketPolicyWithContext(ctx, &s3.GetBucketPolicyInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != "NoSuchBucketPolicy" {
			lib.Logger.Fatal("error: ", err)
		}
	} else {
		descr.Policy = &lib.IamPolicyDocument{}
		err := json.Unmarshal([]byte(*policy.Policy), descr.Policy)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
	}

	replication, err := s3Client.GetBucketReplicationWithContext(ctx, &s3.GetBucketReplicationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != "ReplicationConfigurationNotFoundError" {
			lib.Logger.Fatal("error: ", err)
		}
	} else {
		descr.Replication = replication.ReplicationConfiguration
	}

	fmt.Println(lib.Pformat(descr))
}
