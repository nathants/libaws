package lib

import (
	"context"
	"errors"
	"fmt"
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

// The parent owns all three guests and has already registered independent
// compute cleanup. No SSH ingress or guest credentials are introduced here.
func TestEC2WaitGoSshScopeIntegration(t *testing.T) {
	uid := ec2ExampleUID(t)
	ids := strings.Fields(os.Getenv("LIBAWS_EC2_WAIT_TEST_INSTANCES"))
	if len(ids) != 3 || ids[0] == ids[1] || ids[0] == ids[2] || ids[1] == ids[2] {
		t.Fatal("parent must provide three distinct owned instances")
	}
	t.Setenv("PATH", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
	defer cancel()
	realClient := EC2Client()
	if err := ec2.NewInstanceRunningWaiter(realClient).Wait(ctx, &ec2.DescribeInstancesInput{InstanceIds: ids}, time.Minute); err != nil {
		t.Fatal(err)
	}
	readInstances := func() map[string]ec2types.Instance {
		t.Helper()
		out, err := realClient.DescribeInstances(ctx, &ec2.DescribeInstancesInput{InstanceIds: ids})
		if err != nil {
			t.Fatal(err)
		}
		instances := map[string]ec2types.Instance{}
		for _, reservation := range out.Reservations {
			for _, instance := range reservation.Instances {
				id := aws.ToString(instance.InstanceId)
				if !slices.Contains(ids, id) || aws.ToString(instance.KeyName) != "test-keypair-"+uid || instance.State == nil {
					t.Fatalf("instance is not owned by the example: %s", id)
				}
				instances[id] = instance
			}
		}
		if len(instances) != 3 {
			t.Fatalf("missing owned instances: %d", len(instances))
		}
		return instances
	}
	instances := readInstances()
	for _, instance := range instances {
		if instance.State.Name != ec2types.InstanceStateNameRunning || aws.ToString(instance.PublicDnsName) == "" {
			t.Fatalf("fixture is not running with public DNS: %s", aws.ToString(instance.InstanceId))
		}
	}
	goodID, badID, unrelatedID := ids[0], ids[1], ids[2]
	goodAddr, badAddr := aws.ToString(instances[goodID].PublicDnsName), aws.ToString(instances[badID].PublicDnsName)
	var groupIDs []string
	for _, group := range instances[badID].SecurityGroups {
		groupIDs = append(groupIDs, aws.ToString(group.GroupId))
	}
	if len(groupIDs) != 1 {
		t.Fatal("unready fixture must have only the example's no-ingress security group")
	}
	groups, err := realClient.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{GroupIds: groupIDs})
	if err != nil || len(groups.SecurityGroups) != 1 || aws.ToString(groups.SecurityGroups[0].GroupName) != "test-sg-"+uid || len(groups.SecurityGroups[0].IpPermissions) != 0 {
		t.Fatalf("unready fixture must have no ingress: %v", err)
	}
	key, err := os.ReadFile(os.Getenv("LIBAWS_EC2_WAIT_TEST_KEY"))
	if err != nil {
		t.Fatal(err)
	}
	// Even a regression must not let this test terminate another fixture or
	// any unrelated account resource. A blocked request still fails the test.
	reads, terminations := 0, 0
	pruning := false
	omitID := ""
	opts := realClient.Options()
	opts.APIOptions = append(opts.APIOptions, func(stack *middleware.Stack) error {
		return stack.Initialize.Add(middleware.InitializeMiddlewareFunc("guard SSH wait fixture", func(callCtx context.Context, in middleware.InitializeInput, next middleware.InitializeHandler) (middleware.InitializeOutput, middleware.Metadata, error) {
			if _, ok := in.Parameters.(*ec2.DescribeInstancesInput); ok {
				reads++
			}
			if params, ok := in.Parameters.(*ec2.TerminateInstancesInput); ok {
				terminations++
				if !pruning || !slices.Equal(params.InstanceIds, []string{badID}) {
					return middleware.InitializeOutput{}, middleware.Metadata{}, fmt.Errorf("blocked unsafe SSH wait termination: %v", params.InstanceIds)
				}
			}
			out, metadata, err := next.HandleInitialize(callCtx, in)
			if result, ok := out.Result.(*ec2.DescribeInstancesOutput); ok && err == nil && omitID != "" {
				// Model one incomplete visibility read after a real AWS lookup.
				// Subsequent responses are untouched and must recover the same ID.
				for i := range result.Reservations {
					result.Reservations[i].Instances = slices.DeleteFunc(result.Reservations[i].Instances, func(instance ec2types.Instance) bool {
						return aws.ToString(instance.InstanceId) == omitID
					})
				}
				omitID = ""
			}
			return out, metadata, err
		}), middleware.Before)
	})
	ec2Client = ec2.New(opts)
	t.Cleanup(func() { ec2Client = realClient })
	for _, test := range []struct{ name, user, key, message string }{
		{"missing-user", "", string(key), "expected user"},
		{"missing-key", "fixture", "", "must be provided"},
		{"invalid-key", "fixture", "invalid", "parse SSH private key"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ready, err := EC2WaitGoSsh(ctx, &EC2WaitGoSshInput{Selectors: ids[:2], MaxWaitSeconds: 1, User: test.user, Ed25519PrivKey: test.key})
			if err == nil || !strings.Contains(err.Error(), test.message) || len(ready) != 0 || reads != 0 || terminations != 0 {
				t.Fatalf("configuration failure reached AWS: ready=%v error=%v reads=%d terminations=%d", ready, err, reads, terminations)
			}
		})
	}
	if t.Failed() {
		return
	}
	if !t.Run("initially-incomplete-explicit-IDs", func(t *testing.T) {
		omitID = badID
		ready, err := ec2WaitGoSsh(ctx, &EC2WaitGoSshInput{Selectors: ids[:2], MaxWaitSeconds: 1, User: "fixture", Ed25519PrivKey: string(key)}, func(_ context.Context, input *EC2GoSshInput) ([]*ec2GoSshResult, error) {
			if reads != 2 || len(input.TargetAddrs) != 2 || !slices.Contains(input.TargetAddrs, goodAddr) || !slices.Contains(input.TargetAddrs, badAddr) {
				t.Fatalf("SSH started without the complete live ID selection: reads=%d targets=%v", reads, input.TargetAddrs)
			}
			return []*ec2GoSshResult{{TargetAddr: goodAddr}, {TargetAddr: badAddr}}, nil
		})
		slices.Sort(ready)
		expected := slices.Clone(ids[:2])
		slices.Sort(expected)
		if err != nil || !slices.Equal(ready, expected) || reads != 2 || terminations != 0 || omitID != "" {
			t.Fatalf("incomplete initial lookup lost an explicit ID: ready=%v error=%v reads=%d terminations=%d", ready, err, reads, terminations)
		}
	}) {
		return
	}
	reads = 0
	pruning = true
	ready, err := ec2WaitGoSsh(ctx, &EC2WaitGoSshInput{Selectors: ids[:2], MaxWaitSeconds: 1, User: "fixture", Ed25519PrivKey: string(key)}, func(callCtx context.Context, input *EC2GoSshInput) ([]*ec2GoSshResult, error) {
		if len(input.TargetAddrs) != 2 || !slices.Contains(input.TargetAddrs, goodAddr) || !slices.Contains(input.TargetAddrs, badAddr) {
			t.Fatalf("unexpected live SSH targets: %v", input.TargetAddrs)
		}
		// Exercise a real SSH connection failure to the no-ingress guest. Only
		// the ready host's SSH result is simulated; AWS lookup and pruning are real.
		failedInput := *input
		failedInput.TargetAddrs = []string{badAddr}
		results, err := EC2GoSsh(callCtx, &failedInput)
		if err == nil || len(results) != 1 || results[0].Err == nil || results[0].TargetAddr != badAddr {
			t.Fatalf("expected a real per-host SSH failure: %v %v", results, err)
		}
		// Expire max-wait even if the network rejected the connection immediately.
		time.Sleep(time.Second)
		return append(results, &ec2GoSshResult{TargetAddr: goodAddr}), err
	})
	if err != nil || !slices.Equal(ready, []string{goodID}) || reads != 1 || terminations != 1 {
		t.Fatalf("wrong live pruning: ready=%v error=%v reads=%d terminations=%d", ready, err, reads, terminations)
	}
	if err := ec2.NewInstanceTerminatedWaiter(realClient).Wait(ctx, &ec2.DescribeInstancesInput{InstanceIds: []string{badID}}, 3*time.Minute); err != nil {
		t.Fatal(err)
	}
	instances = readInstances()
	if instances[badID].State.Name != ec2types.InstanceStateNameTerminated || instances[goodID].State.Name != ec2types.InstanceStateNameRunning || instances[unrelatedID].State.Name != ec2types.InstanceStateNameRunning {
		t.Fatal("pruning did not preserve both the ready and unselected guests")
	}
	t.Logf("terminated only %s; ready %s and unselected %s remain running", badID, goodID, unrelatedID)

	t.Run("terminal-transition-keeps-survivor", func(t *testing.T) {
		// The previously unselected guest now becomes the owned interruption
		// fixture, after its survival in the pruning case has been asserted.
		interruptedAddr := aws.ToString(instances[unrelatedID].PublicDnsName)
		reads, terminations = 0, 0
		pruning = false
		probes := 0
		ready, err := ec2WaitGoSsh(ctx, &EC2WaitGoSshInput{Selectors: []string{goodID, unrelatedID}, User: "fixture", Ed25519PrivKey: string(key)}, func(callCtx context.Context, input *EC2GoSshInput) ([]*ec2GoSshResult, error) {
			probes++
			if probes == 1 {
				if len(input.TargetAddrs) != 2 || !slices.Contains(input.TargetAddrs, goodAddr) || !slices.Contains(input.TargetAddrs, interruptedAddr) {
					t.Fatalf("unexpected pre-interruption SSH targets: %v", input.TargetAddrs)
				}
				if _, err := realClient.TerminateInstances(callCtx, &ec2.TerminateInstancesInput{InstanceIds: []string{unrelatedID}}); err != nil {
					t.Fatal(err)
				}
				// Wait only for fixture visibility, not for a failing wait assertion.
				if err := ec2.NewInstanceTerminatedWaiter(realClient).Wait(callCtx, &ec2.DescribeInstancesInput{InstanceIds: []string{unrelatedID}}, 3*time.Minute); err != nil {
					t.Fatal(err)
				}
				interrupted := errors.New("owned guest terminated during readiness wait")
				return []*ec2GoSshResult{{TargetAddr: goodAddr}, {TargetAddr: interruptedAddr, Err: interrupted}}, interrupted
			}
			if !slices.Equal(input.TargetAddrs, []string{goodAddr}) {
				t.Fatalf("terminal guest was retained as an SSH target: %v", input.TargetAddrs)
			}
			return []*ec2GoSshResult{{TargetAddr: goodAddr}}, nil
		})
		if err != nil || !slices.Equal(ready, []string{goodID}) || probes != 2 || reads != 2 || terminations != 0 {
			t.Fatalf("interruption lost the healthy survivor: ready=%v error=%v probes=%d reads=%d terminations=%d", ready, err, probes, reads, terminations)
		}
		instances = readInstances()
		if instances[goodID].State.Name != ec2types.InstanceStateNameRunning || instances[unrelatedID].State.Name != ec2types.InstanceStateNameTerminated {
			t.Fatal("interruption did not preserve the healthy guest")
		}
	})
}
