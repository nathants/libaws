package lib

import (
	"context"
	"errors"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

// TestS3EC2ExampleCleanup is the live example's compute finalizer. Fleet requests
// are not infrastructure-set members and can still be pending with no instances.
func TestS3EC2ExampleCleanup(t *testing.T) {
	uid := os.Getenv("LIBAWS_S3_EC2_CLEANUP_UID")
	if uid == "" {
		t.Skip("run through examples/complex/s3-ec2")
	}
	if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(uid) {
		t.Fatal("cleanup requires a unique 12-hex example uid")
	}
	requireLiveAWSAccount(t)
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	defer cancel()
	function := "test-lambda-" + uid
	timeout := int32(lambdaAttrTimeoutDefault)
	configuration, err := LambdaClient().GetFunctionConfiguration(ctx, &lambda.GetFunctionConfigurationInput{FunctionName: aws.String(function)})
	if err != nil {
		var missing *lambdatypes.ResourceNotFoundException
		if !errors.As(err, &missing) {
			t.Error(err)
		}
	} else if configuration.Timeout != nil {
		timeout = *configuration.Timeout
	}
	// Stop new invocations before draining any invocation that might already
	// have begun. Deleting a function does not kill its in-flight invocation.
	if err := LambdaDelete(ctx, function, false); err != nil {
		t.Error(err)
	}
	if os.Getenv("LIBAWS_S3_EC2_DRAIN") == "yes" {
		t.Logf("draining in-flight Lambda work for %d seconds", timeout)
		select {
		case <-time.After(time.Duration(timeout+1) * time.Second):
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	cleanupEC2ExampleCompute(t, ctx, "test-keypair-"+uid)
}
