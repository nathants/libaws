package lib

import (
	"context"
	"errors"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/smithy-go/middleware"
)

func ec2ExampleUID(t *testing.T) string {
	t.Helper()
	uid := os.Getenv("LIBAWS_EC2_EXAMPLE_UID")
	if uid == "" {
		t.Skip("run through examples/misc/ec2 or its live failure-cleanup test")
	}
	if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(uid) {
		t.Fatal("EC2 example requires a unique 12-hex uid")
	}
	requireLiveAWSAccount(t)
	return uid
}

func ec2ExampleConfig(t *testing.T, ctx context.Context, uid string) *EC2Config {
	t.Helper()
	vpcName, key := "vpc-test-"+uid, "test-keypair-"+uid
	vpcID, err := VpcID(ctx, vpcName)
	if err != nil {
		t.Fatal(err)
	}
	vpcs, err := EC2Client().DescribeVpcs(ctx, &ec2.DescribeVpcsInput{VpcIds: []string{vpcID}})
	if err != nil || len(vpcs.Vpcs) != 1 || EC2GetTag(vpcs.Vpcs[0].Tags, infraSetTagName, "") != "test-ec2-subnets-"+uid {
		t.Fatalf("VPC is not owned by the EC2 example: %v", err)
	}
	keys, err := EC2Client().DescribeKeyPairs(ctx, &ec2.DescribeKeyPairsInput{KeyNames: []string{key}})
	if err != nil || len(keys.KeyPairs) != 1 || EC2GetTag(keys.KeyPairs[0].Tags, infraSetTagName, "") != "test-ec2-subnets-"+uid {
		t.Fatalf("keypair is not owned by the EC2 example: %v", err)
	}
	sg, err := EC2SgID(ctx, vpcName, "test-sg-"+uid)
	if err != nil {
		t.Fatal(err)
	}
	subnets, err := EC2SubnetsFromVpc(ctx, vpcID, ec2types.InstanceTypeT3Small, true)
	if err != nil {
		t.Fatal(err)
	}
	ami, user, err := EC2AmiBase(ctx, EC2AmiDebianTrixie, EC2ArchAmd64)
	if err != nil {
		t.Fatal(err)
	}
	return &EC2Config{
		NumInstances: 1, Name: "test-ec2-failure-" + uid, UserName: user, Key: key,
		SgID: sg, AmiID: ami, InstanceType: ec2types.InstanceTypeT3Small,
		SubnetIds: subnets, Gigs: 8, SecondsTimeout: 600,
	}
}

func TestEC2ExampleCleanup(t *testing.T) {
	uid := ec2ExampleUID(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	cleanupEC2ExampleCompute(t, ctx, "test-keypair-"+uid)
}

func TestEC2ExampleComputeAbsent(t *testing.T) {
	uid := ec2ExampleUID(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	assertEC2ExampleComputeAbsent(t, ctx, "test-keypair-"+uid)
	// Prove that the specific request created before the simulated lost
	// response was canceled, not just absent from a tag-based inventory.
	data, err := os.ReadFile(os.Getenv("LIBAWS_EC2_EXAMPLE_FLEET_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	id := string(data)
	fleet, err := EC2DescribeSpotFleet(ctx, &id)
	if err != nil || !slices.Contains(ec2FailedStates, fleet.SpotFleetRequestState) {
		t.Fatalf("empty fleet was not canceled: %v %v", fleet, err)
	}
}

// The Python parent has already registered cleanup and owns this fixture. A
// zero-capacity fleet keeps an active AWS request without launching any guests;
// the parent then injects a CLI failure that returned no instance ID.
func TestEC2ExampleEmptyFleet(t *testing.T) {
	uid := ec2ExampleUID(t)
	path := os.Getenv("LIBAWS_EC2_EXAMPLE_FLEET_FILE")
	if path == "" {
		t.Fatal("parent must provide its private fleet-ID file")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	config := ec2ExampleConfig(t, ctx, uid)
	handedOff := false
	t.Cleanup(func() {
		if !handedOff {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 8*time.Minute)
			defer cleanupCancel()
			cleanupEC2ExampleCompute(t, cleanupCtx, config.Key)
		}
	})
	role, err := IamClient().GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(EC2SpotFleetTaggingRole)})
	if err != nil {
		t.Fatal(err)
	}
	fleet, err := EC2Client().RequestSpotFleet(ctx, &ec2.RequestSpotFleetInput{SpotFleetRequestConfig: &ec2types.SpotFleetRequestConfigData{
		IamFleetRole: role.Role.Arn, AllocationStrategy: ec2types.AllocationStrategyLowestPrice,
		Type: ec2types.FleetTypeMaintain, TargetCapacity: aws.Int32(0),
		ValidUntil:                       aws.Time(time.Now().UTC().Add(time.Hour).Truncate(time.Second)),
		TerminateInstancesWithExpiration: aws.Bool(true),
		LaunchSpecifications: []ec2types.SpotFleetLaunchSpecification{{
			ImageId: aws.String(config.AmiID), KeyName: aws.String(config.Key),
			SubnetId: aws.String(config.SubnetIds[0]), InstanceType: config.InstanceType,
			SecurityGroups:      []ec2types.GroupIdentifier{{GroupId: aws.String(config.SgID)}},
			BlockDeviceMappings: makeBlockDeviceMapping(config),
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EC2DescribeSpotFleet(ctx, fleet.SpotFleetRequestId); err != nil {
		t.Fatal(err)
	}
	instances, err := EC2DescribeSpotFleetActiveInstances(ctx, fleet.SpotFleetRequestId)
	if err != nil || len(instances) != 0 {
		t.Fatalf("zero-capacity fleet unexpectedly launched instances: %v %v", instances, err)
	}
	if err := os.WriteFile(path, []byte(*fleet.SpotFleetRequestId), 0600); err != nil {
		t.Fatal(err)
	}
	handedOff = true
	t.Logf("created empty fleet %s for parent-owned failure cleanup", *fleet.SpotFleetRequestId)
}

func TestEC2SpotFleetFailureIntegration(t *testing.T) {
	uid := ec2ExampleUID(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	config := ec2ExampleConfig(t, ctx, uid)
	t.Run("per-fleet-error", func(t *testing.T) {
		// Non-hex characters cannot identify an AWS-assigned fleet UUID, but
		// this shape reaches per-fleet validation rather than HTTP validation.
		id := "sfr-zzzzzzzz-zzzz-zzzz-zzzz-" + uid
		out, err := EC2Client().CancelSpotFleetRequests(ctx, &ec2.CancelSpotFleetRequestsInput{SpotFleetRequestIds: []string{id}, TerminateInstances: aws.Bool(false)})
		if err != nil || len(out.UnsuccessfulFleetRequests) != 1 {
			t.Fatalf("expected an AWS per-fleet rejection: %v %v", out, err)
		}
		code := string(out.UnsuccessfulFleetRequests[0].Error.Code)
		if err := ec2CancelSpotFleet(ctx, &id, false); err == nil || !strings.Contains(err.Error(), code) {
			t.Fatalf("AWS cancellation error was lost: %v", err)
		}
	})
	for _, phase := range []string{"finalize", "after-finalize", "empty-instance-list"} {
		t.Run(phase, func(t *testing.T) {
			realClient := EC2Client()
			launchCtx, launchCancel := context.WithCancel(ctx)
			defer launchCancel()
			injected := errors.New("injected EC2 " + phase + " failure")
			var id *string
			fired, finalized := false, false
			opts := realClient.Options()
			opts.APIOptions = append(opts.APIOptions, func(stack *middleware.Stack) error {
				return stack.Initialize.Add(middleware.InitializeMiddlewareFunc("example launch failure", func(callCtx context.Context, in middleware.InitializeInput, next middleware.InitializeHandler) (middleware.InitializeOutput, middleware.Metadata, error) {
					params, cancellation := in.Parameters.(*ec2.CancelSpotFleetRequestsInput)
					finalizing := cancellation && !aws.ToBool(params.TerminateInstances)
					if finalizing && phase == "finalize" {
						fired = true
						return middleware.InitializeOutput{}, middleware.Metadata{}, injected
					}
					out, metadata, err := next.HandleInitialize(callCtx, in)
					if result, ok := out.Result.(*ec2.RequestSpotFleetOutput); ok && err == nil {
						id = result.SpotFleetRequestId
					}
					if finalizing && err == nil {
						finalized = true
						if phase == "after-finalize" {
							fired = true
							launchCancel()
							return out, metadata, injected
						}
					}
					if result, ok := out.Result.(*ec2.DescribeSpotFleetInstancesOutput); ok && err == nil && finalized && phase == "empty-instance-list" && !fired {
						// Model an empty control-plane read after AWS accepted and
						// finalized a real fleet. Cleanup still sees the actual guests.
						result.ActiveInstances = nil
						fired = true
					}
					return out, metadata, err
				}), middleware.Before)
			})
			ec2Client = ec2.New(opts)
			t.Cleanup(func() {
				ec2Client = realClient
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 8*time.Minute)
				defer cleanupCancel()
				cleanupEC2ExampleCompute(t, cleanupCtx, config.Key)
			})
			instances, err := EC2RequestSpotFleet(launchCtx, ec2types.AllocationStrategyLowestPrice, config)
			expectedError := errors.Is(err, injected)
			if phase == "empty-instance-list" {
				expectedError = err != nil && strings.Contains(err.Error(), "returned no instances")
			}
			if !fired || id == nil || len(instances) != 0 || !expectedError {
				t.Fatalf("live post-create failure was not exercised: fired=%t fleet=%v instances=%v error=%v", fired, id, instances, err)
			}
			fleet, err := EC2DescribeSpotFleet(ctx, id)
			if err != nil || !slices.Contains(ec2FailedStates, fleet.SpotFleetRequestState) {
				t.Fatalf("failed launch left its fleet active: %v %v", fleet, err)
			}
			// Assert before the safety finalizer, so it cannot hide a library leak.
			assertEC2ExampleComputeAbsent(t, ctx, config.Key)
		})
	}
}
