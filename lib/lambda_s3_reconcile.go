package lib

import (
	"context"
	"fmt"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type lambdaS3NotificationClient interface {
	GetBucketNotificationConfiguration(
		context.Context,
		*s3.GetBucketNotificationConfigurationInput,
		...func(*s3.Options),
	) (*s3.GetBucketNotificationConfigurationOutput, error)
	PutBucketNotificationConfiguration(
		context.Context,
		*s3.PutBucketNotificationConfigurationInput,
		...func(*s3.Options),
	) (*s3.PutBucketNotificationConfigurationOutput, error)
}

type lambdaS3AccountClient interface {
	ListBuckets(
		context.Context,
		*s3.ListBucketsInput,
		...func(*s3.Options),
	) (*s3.ListBucketsOutput, error)
}

type lambdaS3ClientForBucket func(context.Context, string) (lambdaS3NotificationClient, error)

func lambdaAWSClientForBucket(ctx context.Context, bucket string) (lambdaS3NotificationClient, error) {
	return S3ClientBucketRegion(ctx, bucket)
}

func lambdaEnsureDesiredS3Trigger(
	ctx context.Context,
	clientForBucket lambdaS3ClientForBucket,
	infraLambda *InfraLambda,
	bucket string,
	events []s3types.Event,
	preview bool,
) error {
	client, err := clientForBucket(ctx, bucket)
	if err != nil {
		if !preview || !isS3NoSuchBucket(err) {
			return err
		}
		client = nil
	}
	var out *s3.GetBucketNotificationConfigurationOutput
	if client != nil {
		out, err = client.GetBucketNotificationConfiguration(ctx, &s3.GetBucketNotificationConfigurationInput{
			Bucket: aws.String(bucket),
		})
		if err != nil {
			if !preview || !isS3NoSuchBucket(err) {
				return err
			}
			out = nil
		}
	}
	if out == nil {
		out = &s3.GetBucketNotificationConfigurationOutput{
			LambdaFunctionConfigurations: []s3types.LambdaFunctionConfiguration{},
		}
	}
	var existingEvents []s3types.Event
	for _, conf := range out.LambdaFunctionConfigurations {
		if aws.ToString(conf.LambdaFunctionArn) == infraLambda.Arn {
			existingEvents = conf.Events
		}
	}
	if slices.Equal(existingEvents, events) {
		return nil
	}
	var configurations []s3types.LambdaFunctionConfiguration
	for _, conf := range out.LambdaFunctionConfigurations {
		if aws.ToString(conf.LambdaFunctionArn) != infraLambda.Arn {
			configurations = append(configurations, conf)
		}
	}
	configurations = append(configurations, s3types.LambdaFunctionConfiguration{
		LambdaFunctionArn: aws.String(infraLambda.Arn),
		Events:            events,
	})
	if !preview && client != nil {
		err := Retry(ctx, func() error {
			_, err := client.PutBucketNotificationConfiguration(ctx, &s3.PutBucketNotificationConfigurationInput{
				Bucket: aws.String(bucket),
				NotificationConfiguration: &s3types.NotificationConfiguration{
					LambdaFunctionConfigurations: configurations,
					EventBridgeConfiguration:     out.EventBridgeConfiguration,
					QueueConfigurations:          out.QueueConfigurations,
					TopicConfigurations:          out.TopicConfigurations,
				},
			})
			return err
		})
		if err != nil {
			return err
		}
	}
	Logger.Printf(PreviewString(preview)+"updated bucket notifications for %s %s: %s => %s\n",
		bucket, infraLambda.Name, existingEvents, events)
	return nil
}

func lambdaRemoveStaleS3Triggers(
	ctx context.Context,
	accountClient lambdaS3AccountClient,
	clientForBucket lambdaS3ClientForBucket,
	infraLambda *InfraLambda,
	desiredBuckets []string,
	preview bool,
) error {
	buckets, err := accountClient.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return err
	}
	if buckets == nil {
		return fmt.Errorf("list S3 buckets returned nil output")
	}
	for _, bucket := range buckets.Buckets {
		name := aws.ToString(bucket.Name)
		if slices.Contains(desiredBuckets, name) {
			continue
		}
		client, err := clientForBucket(ctx, name)
		if err != nil {
			if isS3NoSuchBucket(err) {
				continue
			}
			return err
		}
		out, err := client.GetBucketNotificationConfiguration(ctx, &s3.GetBucketNotificationConfigurationInput{
			Bucket: bucket.Name,
		})
		if err != nil {
			if isS3NoSuchBucket(err) {
				continue
			}
			return err
		}
		if out == nil {
			return fmt.Errorf("get S3 bucket notification configuration for %q returned nil output", name)
		}
		var configurations []s3types.LambdaFunctionConfiguration
		for _, conf := range out.LambdaFunctionConfigurations {
			if aws.ToString(conf.LambdaFunctionArn) != infraLambda.Arn {
				configurations = append(configurations, conf)
			} else {
				Logger.Println(PreviewString(preview)+"deleted bucket notification:", infraLambda.Name, name)
			}
		}
		if len(configurations) == len(out.LambdaFunctionConfigurations) || preview {
			continue
		}
		_, err = client.PutBucketNotificationConfiguration(ctx, &s3.PutBucketNotificationConfigurationInput{
			Bucket: bucket.Name,
			NotificationConfiguration: &s3types.NotificationConfiguration{
				LambdaFunctionConfigurations: configurations,
				EventBridgeConfiguration:     out.EventBridgeConfiguration,
				QueueConfigurations:          out.QueueConfigurations,
				TopicConfigurations:          out.TopicConfigurations,
			},
		})
		if err != nil {
			if isS3NoSuchBucket(err) {
				continue
			}
			return err
		}
	}
	return nil
}
