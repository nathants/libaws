package lib

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
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
	key := "test-keypair-" + uid
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
	fleets := ec2.NewDescribeSpotFleetRequestsPaginator(EC2Client(), &ec2.DescribeSpotFleetRequestsInput{})
	for fleets.HasMorePages() {
		page, err := fleets.NextPage(ctx)
		if err != nil {
			t.Error(err)
			break
		}
		for _, fleet := range page.SpotFleetRequestConfigs {
			if fleet.SpotFleetRequestConfig == nil || strings.HasPrefix(string(fleet.SpotFleetRequestState), "cancelled") {
				continue
			}
			specifications := fleet.SpotFleetRequestConfig.LaunchSpecifications
			owned := len(specifications) > 0
			for _, specification := range specifications {
				owned = owned && aws.ToString(specification.KeyName) == key
			}
			if !owned {
				continue
			}
			result, err := EC2Client().CancelSpotFleetRequests(ctx, &ec2.CancelSpotFleetRequestsInput{
				SpotFleetRequestIds: []string{aws.ToString(fleet.SpotFleetRequestId)},
				TerminateInstances:  aws.Bool(true),
			})
			if err != nil {
				t.Error(err)
			} else if len(result.UnsuccessfulFleetRequests) != 0 || len(result.SuccessfulFleetRequests) != 1 {
				t.Errorf("cancel owned fleet %s: %+v", aws.ToString(fleet.SpotFleetRequestId), result)
			}
		}
	}
	// Match the unique fixture key, not mutable infrastructure membership tags.
	instances := ec2.NewDescribeInstancesPaginator(EC2Client(), &ec2.DescribeInstancesInput{
		Filters: []ec2types.Filter{{Name: aws.String("key-name"), Values: []string{key}}},
	})
	var terminate, wait []string
	for instances.HasMorePages() {
		page, err := instances.NextPage(ctx)
		if err != nil {
			t.Error(err)
			break
		}
		for _, reservation := range page.Reservations {
			for _, instance := range reservation.Instances {
				if instance.State.Name == ec2types.InstanceStateNameTerminated {
					continue
				}
				id := aws.ToString(instance.InstanceId)
				wait = append(wait, id)
				if instance.State.Name != ec2types.InstanceStateNameShuttingDown {
					terminate = append(terminate, id)
				}
			}
		}
	}
	if len(terminate) > 0 {
		if _, err := EC2Client().TerminateInstances(ctx, &ec2.TerminateInstancesInput{InstanceIds: terminate}); err != nil {
			t.Error(err)
		}
	}
	if len(wait) > 0 {
		if err := EC2WaitState(ctx, wait, ec2types.InstanceStateNameTerminated); err != nil {
			t.Error(err)
		}
	}
}
