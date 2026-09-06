package lib

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

func lambdaZipHashForTest(data []byte) string {
	hash := sha256.Sum256(data)
	return base64.StdEncoding.EncodeToString(hash[:])
}

func requireLiveAWSAccount(t *testing.T) {
	t.Helper()
	expectedAccount := os.Getenv("LIBAWS_TEST_ACCOUNT")
	if expectedAccount == "" {
		t.Skip("set LIBAWS_TEST_ACCOUNT to run live AWS tests")
	}
	account, err := StsAccount(context.Background())
	if err != nil {
		t.Fatalf("verify AWS account: %v", err)
	}
	if account != expectedAccount {
		t.Fatalf("AWS account = %s, want guarded account %s", account, expectedAccount)
	}
}

// S3 can alternate new and old configuration after a write. Wait for stable
// fixture reads before asserting behavior once; never retry failed assertions.
func waitLiveAWSFixtureStable(ctx context.Context, ready func() (bool, error)) error {
	var stableSince time.Time
	for {
		ok, err := ready()
		if err != nil {
			return err
		}
		if !ok {
			stableSince = time.Time{}
		} else if stableSince.IsZero() {
			stableSince = time.Now()
		} else if time.Since(stableSince) >= 10*time.Second {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func TestDropLinesWithAny(t *testing.T) {
	type test struct {
		input  string
		output string
		tokens []string
	}
	tests := []test{
		{"a\nb\nc\n", "a\nb\nc\n", []string{"foo"}},
		{"a\nb\nc\n", "b\nc\n", []string{"a"}},
		{"a\nb\nc\n", "b\n", []string{"a", "c"}},
	}
	for _, test := range tests {
		output := DropLinesWithAny(test.input, test.tokens...)
		if output != test.output {
			t.Errorf("\ngot:\n%s\nwant:\n%s\n", output, test.output)
		}
	}
}

// Fleet requests have no infrastructure-set membership. The unique fixture key
// proves ownership even before instances exist or after membership tags are lost.
func ec2ExampleOwnedFleets(ctx context.Context, key string) ([]ec2types.SpotFleetRequestConfig, error) {
	var owned []ec2types.SpotFleetRequestConfig
	pages := ec2.NewDescribeSpotFleetRequestsPaginator(EC2Client(), &ec2.DescribeSpotFleetRequestsInput{})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return owned, err
		}
		for _, fleet := range page.SpotFleetRequestConfigs {
			if fleet.SpotFleetRequestConfig == nil {
				continue
			}
			specifications := fleet.SpotFleetRequestConfig.LaunchSpecifications
			matches := len(specifications) > 0
			for _, specification := range specifications {
				matches = matches && aws.ToString(specification.KeyName) == key
			}
			if matches {
				owned = append(owned, fleet)
			}
		}
	}
	return owned, nil
}

func ec2ExampleOwnedInstances(ctx context.Context, key string) ([]ec2types.Instance, error) {
	var instances []ec2types.Instance
	pages := ec2.NewDescribeInstancesPaginator(EC2Client(), &ec2.DescribeInstancesInput{
		Filters: []ec2types.Filter{{Name: aws.String("key-name"), Values: []string{key}}},
	})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return instances, err
		}
		for _, reservation := range page.Reservations {
			instances = append(instances, reservation.Instances...)
		}
	}
	return instances, nil
}

func assertEC2ExampleComputeAbsent(t *testing.T, ctx context.Context, key string) {
	t.Helper()
	fleets, err := ec2ExampleOwnedFleets(ctx, key)
	if err != nil {
		t.Error(err)
	}
	for _, fleet := range fleets {
		// All canceled states prevent new launches. The suffix describes slow
		// bookkeeping; actual instance termination is checked independently below.
		if !slices.Contains(ec2FailedStates, fleet.SpotFleetRequestState) {
			t.Errorf("owned fleet %s can still launch: %s", aws.ToString(fleet.SpotFleetRequestId), fleet.SpotFleetRequestState)
		}
	}
	instances, err := ec2ExampleOwnedInstances(ctx, key)
	if err != nil {
		t.Error(err)
	}
	for _, instance := range instances {
		if instance.State.Name != ec2types.InstanceStateNameTerminated {
			t.Errorf("owned instance %s remains %s", aws.ToString(instance.InstanceId), instance.State.Name)
		}
	}
}

func cleanupEC2ExampleCompute(t *testing.T, ctx context.Context, key string) {
	t.Helper()
	if !regexp.MustCompile(`^test-keypair-[0-9a-f]{12}$`).MatchString(key) {
		t.Fatal("compute cleanup requires a unique example keypair")
	}
	fleets, err := ec2ExampleOwnedFleets(ctx, key)
	if err != nil {
		t.Error(err)
	}
	for _, fleet := range fleets {
		if fleet.SpotFleetRequestState == ec2types.BatchStateCancelled {
			continue
		}
		if err := EC2TeardownSpotFleet(ctx, fleet.SpotFleetRequestId); err != nil {
			t.Error(err)
		}
	}
	// Independent discovery also covers on-demand instances and any fleet whose
	// cancellation/listing failed. Attempt it even when another cleanup failed.
	instances, err := ec2ExampleOwnedInstances(ctx, key)
	if err != nil {
		t.Error(err)
	}
	var terminate, wait []string
	for _, instance := range instances {
		if instance.State.Name == ec2types.InstanceStateNameTerminated {
			continue
		}
		id := aws.ToString(instance.InstanceId)
		wait = append(wait, id)
		if instance.State.Name != ec2types.InstanceStateNameShuttingDown {
			terminate = append(terminate, id)
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
	assertEC2ExampleComputeAbsent(t, ctx, key)
}
