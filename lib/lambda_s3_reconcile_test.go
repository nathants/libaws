package lib

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type lambdaS3TestAPIError struct {
	code string
}

func (err lambdaS3TestAPIError) Error() string     { return "S3 API error: " + err.code }
func (err lambdaS3TestAPIError) ErrorCode() string { return err.code }

type fakeLambdaS3AccountClient struct {
	buckets []string
}

func (client *fakeLambdaS3AccountClient) ListBuckets(
	context.Context,
	*s3.ListBucketsInput,
	...func(*s3.Options),
) (*s3.ListBucketsOutput, error) {
	out := &s3.ListBucketsOutput{}
	for _, bucket := range client.buckets {
		out.Buckets = append(out.Buckets, s3types.Bucket{Name: aws.String(bucket)})
	}
	return out, nil
}

type fakeLambdaS3NotificationClient struct {
	getOut   *s3.GetBucketNotificationConfigurationOutput
	getErr   error
	putErr   error
	putCalls int
	putInput *s3.PutBucketNotificationConfigurationInput
}

func (client *fakeLambdaS3NotificationClient) GetBucketNotificationConfiguration(
	context.Context,
	*s3.GetBucketNotificationConfigurationInput,
	...func(*s3.Options),
) (*s3.GetBucketNotificationConfigurationOutput, error) {
	if client.getErr != nil {
		return nil, client.getErr
	}
	if client.getOut == nil {
		return &s3.GetBucketNotificationConfigurationOutput{}, nil
	}
	return client.getOut, nil
}

func (client *fakeLambdaS3NotificationClient) PutBucketNotificationConfiguration(
	_ context.Context,
	input *s3.PutBucketNotificationConfigurationInput,
	_ ...func(*s3.Options),
) (*s3.PutBucketNotificationConfigurationOutput, error) {
	client.putCalls++
	client.putInput = input
	if client.putErr != nil {
		return nil, client.putErr
	}
	return &s3.PutBucketNotificationConfigurationOutput{}, nil
}

func callLambdaRemoveStaleS3Triggers(
	t *testing.T,
	accountClient lambdaS3AccountClient,
	clientForBucket lambdaS3ClientForBucket,
	infraLambda *InfraLambda,
) error {
	t.Helper()
	return lambdaRemoveStaleS3Triggers(
		context.Background(), accountClient, clientForBucket, infraLambda, nil, false,
	)
}

func TestLambdaRemoveStaleS3TriggersSkipsDesiredBuckets(t *testing.T) {
	resolverCalls := 0
	err := lambdaRemoveStaleS3Triggers(
		context.Background(),
		&fakeLambdaS3AccountClient{buckets: []string{"desired"}},
		func(context.Context, string) (lambdaS3NotificationClient, error) {
			resolverCalls++
			return &fakeLambdaS3NotificationClient{}, nil
		},
		&InfraLambda{Name: "function", Arn: "arn:aws:lambda:region:account:function:function"},
		[]string{"desired"},
		false,
	)
	if err != nil {
		t.Fatalf("stale S3 reconciliation: %v", err)
	}
	if resolverCalls != 0 {
		t.Fatalf("desired bucket client resolutions = %d, want 0", resolverCalls)
	}
}

func TestLambdaRemoveStaleS3TriggersToleratesDeletionDuringRegionLookup(t *testing.T) {
	missing := lambdaS3TestAPIError{code: s3ErrCodeNoSuchBucket}
	err := callLambdaRemoveStaleS3Triggers(
		t,
		&fakeLambdaS3AccountClient{buckets: []string{"deleted"}},
		func(context.Context, string) (lambdaS3NotificationClient, error) { return nil, missing },
		&InfraLambda{Name: "function", Arn: "arn:aws:lambda:region:account:function:function"},
	)
	if err != nil {
		t.Fatalf("deleted bucket reconciliation error = %v", err)
	}
}

func TestLambdaRemoveStaleS3TriggersToleratesDeletionBeforeRead(t *testing.T) {
	missing := lambdaS3TestAPIError{code: s3ErrCodeNoSuchBucket}
	client := &fakeLambdaS3NotificationClient{getErr: missing}
	err := callLambdaRemoveStaleS3Triggers(
		t,
		&fakeLambdaS3AccountClient{buckets: []string{"deleted"}},
		func(context.Context, string) (lambdaS3NotificationClient, error) { return client, nil },
		&InfraLambda{Name: "function", Arn: "arn:aws:lambda:region:account:function:function"},
	)
	if err != nil {
		t.Fatalf("bucket deleted before notification read error = %v", err)
	}
	if client.putCalls != 0 {
		t.Fatalf("notification writes = %d, want 0", client.putCalls)
	}
}

func TestLambdaRemoveStaleS3TriggersToleratesDeletionBeforeWrite(t *testing.T) {
	functionARN := "arn:aws:lambda:region:account:function:function"
	client := &fakeLambdaS3NotificationClient{
		getOut: &s3.GetBucketNotificationConfigurationOutput{
			LambdaFunctionConfigurations: []s3types.LambdaFunctionConfiguration{{
				LambdaFunctionArn: aws.String(functionARN),
				Events:            []s3types.Event{"s3:ObjectCreated:*"},
			}},
		},
		putErr: lambdaS3TestAPIError{code: s3ErrCodeNoSuchBucket},
	}
	resolverCalls := 0
	err := callLambdaRemoveStaleS3Triggers(
		t,
		&fakeLambdaS3AccountClient{buckets: []string{"deleted"}},
		func(context.Context, string) (lambdaS3NotificationClient, error) {
			resolverCalls++
			return client, nil
		},
		&InfraLambda{Name: "function", Arn: functionARN},
	)
	if err != nil {
		t.Fatalf("bucket deleted before notification write error = %v", err)
	}
	if resolverCalls != 1 {
		t.Fatalf("regional client resolutions = %d, want 1", resolverCalls)
	}
	if client.putCalls != 1 {
		t.Fatalf("notification writes = %d, want 1", client.putCalls)
	}
	if got := aws.ToString(client.putInput.Bucket); got != "deleted" {
		t.Fatalf("notification write bucket = %q, want deleted", got)
	}
	if got := len(client.putInput.NotificationConfiguration.LambdaFunctionConfigurations); got != 0 {
		t.Fatalf("remaining Lambda notification configurations = %d, want 0", got)
	}
}

func TestLambdaRemoveStaleS3TriggersPropagatesProviderErrors(t *testing.T) {
	accessDenied := errors.New("access denied")
	functionARN := "arn:aws:lambda:region:account:function:function"
	staleConfiguration := &s3.GetBucketNotificationConfigurationOutput{
		LambdaFunctionConfigurations: []s3types.LambdaFunctionConfiguration{{
			LambdaFunctionArn: aws.String(functionARN),
		}},
	}
	tests := []struct {
		name            string
		clientForBucket lambdaS3ClientForBucket
	}{
		{
			name: "region lookup",
			clientForBucket: func(context.Context, string) (lambdaS3NotificationClient, error) {
				return nil, accessDenied
			},
		},
		{
			name: "notification read",
			clientForBucket: func(context.Context, string) (lambdaS3NotificationClient, error) {
				return &fakeLambdaS3NotificationClient{getErr: accessDenied}, nil
			},
		},
		{
			name: "notification write",
			clientForBucket: func(context.Context, string) (lambdaS3NotificationClient, error) {
				return &fakeLambdaS3NotificationClient{
					getOut: staleConfiguration,
					putErr: accessDenied,
				}, nil
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := callLambdaRemoveStaleS3Triggers(
				t,
				&fakeLambdaS3AccountClient{buckets: []string{"bucket"}},
				test.clientForBucket,
				&InfraLambda{Name: "function", Arn: functionARN},
			)
			if !errors.Is(err, accessDenied) {
				t.Fatalf("error = %v, want %v", err, accessDenied)
			}
		})
	}
}

func TestLambdaEnsureDesiredS3TriggerRejectsMissingBucketOutsidePreview(t *testing.T) {
	missing := lambdaS3TestAPIError{code: s3ErrCodeNoSuchBucket}
	tests := []struct {
		name            string
		clientForBucket lambdaS3ClientForBucket
	}{
		{
			name: "region lookup",
			clientForBucket: func(context.Context, string) (lambdaS3NotificationClient, error) {
				return nil, missing
			},
		},
		{
			name: "notification read",
			clientForBucket: func(context.Context, string) (lambdaS3NotificationClient, error) {
				return &fakeLambdaS3NotificationClient{getErr: missing}, nil
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := lambdaEnsureDesiredS3Trigger(
				context.Background(),
				test.clientForBucket,
				&InfraLambda{Name: "function", Arn: "arn:aws:lambda:region:account:function:function"},
				"missing",
				[]s3types.Event{"s3:ObjectCreated:*"},
				false,
			)
			if !errors.Is(err, missing) {
				t.Fatalf("missing desired bucket error = %v, want %v", err, missing)
			}
		})
	}
}

func TestLambdaEnsureDesiredS3TriggerPreviewModelsOnlyMissingBucket(t *testing.T) {
	infraLambda := &InfraLambda{Name: "function", Arn: "arn:aws:lambda:region:account:function:function"}
	events := []s3types.Event{"s3:ObjectCreated:*"}
	missing := lambdaS3TestAPIError{code: s3ErrCodeNoSuchBucket}
	accessDenied := errors.New("access denied")
	tests := []struct {
		name            string
		clientForBucket lambdaS3ClientForBucket
		wantErr         error
	}{
		{
			name: "missing during region lookup",
			clientForBucket: func(context.Context, string) (lambdaS3NotificationClient, error) {
				return nil, missing
			},
		},
		{
			name: "missing during notification read",
			clientForBucket: func(context.Context, string) (lambdaS3NotificationClient, error) {
				return &fakeLambdaS3NotificationClient{getErr: missing}, nil
			},
		},
		{
			name: "unrelated error during region lookup",
			clientForBucket: func(context.Context, string) (lambdaS3NotificationClient, error) {
				return nil, accessDenied
			},
			wantErr: accessDenied,
		},
		{
			name: "unrelated error during notification read",
			clientForBucket: func(context.Context, string) (lambdaS3NotificationClient, error) {
				return &fakeLambdaS3NotificationClient{getErr: accessDenied}, nil
			},
			wantErr: accessDenied,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := lambdaEnsureDesiredS3Trigger(
				context.Background(), test.clientForBucket, infraLambda, "bucket", events, true,
			)
			if test.wantErr == nil && err != nil {
				t.Fatalf("preview missing desired bucket: %v", err)
			}
			if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("preview provider error = %v, want %v", err, test.wantErr)
			}
		})
	}
}
