package lib

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/iam"
)

var ec2Client *ec2.EC2
var ec2ClientLock sync.RWMutex

func EC2Client() *ec2.EC2 {
	ec2ClientLock.Lock()
	defer ec2ClientLock.Unlock()
	if ec2Client == nil {
		ec2Client = ec2.New(Session())
	}
	return ec2Client
}

type FleetConfig struct {
	NumInstances  int
	Name          string
	SgID          string
	AmiID         string
	Key           string
	InstanceTypes []string
	SubnetIds     []string
}

func RetryDescribeSpotFleet(ctx context.Context, input *ec2.RequestSpotFleetOutput) (*ec2.SpotFleetRequestConfig, error) {
	var output *ec2.DescribeSpotFleetRequestsOutput
	err := Retry(func() error {
		var err error
		output, err = EC2Client().DescribeSpotFleetRequestsWithContext(ctx, &ec2.DescribeSpotFleetRequestsInput{
			SpotFleetRequestIds: []*string{input.SpotFleetRequestId},
		})
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(output.SpotFleetRequestConfigs) != 1 {
		return nil, fmt.Errorf("not the right number of configs: %d", len(output.SpotFleetRequestConfigs))
	}
	return output.SpotFleetRequestConfigs[0], nil
}

func RetryDescribeSpotFleetInstances(ctx context.Context, spotFleetRequestId *string) ([]*ec2.ActiveInstance, error) {
	var instances []*ec2.ActiveInstance
	var nextToken *string
	for {
		var output *ec2.DescribeSpotFleetInstancesOutput
		err := Retry(func() error {
			var err error
			output, err = EC2Client().DescribeSpotFleetInstancesWithContext(ctx, &ec2.DescribeSpotFleetInstancesInput{
				NextToken:          nextToken,
				SpotFleetRequestId: spotFleetRequestId,
			})
			if err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		instances = append(instances, output.ActiveInstances...)
		if output.NextToken == nil {
			break
		}
		nextToken = output.NextToken
	}
	return instances, nil
}

func RetryDescribeInstances(ctx context.Context, instanceIDs []string) ([]*ec2.Instance, error) {
	Assert(len(instanceIDs) < 1000, "cannot list 1000 instances by id")
	var output *ec2.DescribeInstancesOutput
	err := Retry(func() error {
		var err error
		output, err = EC2Client().DescribeInstancesWithContext(ctx, &ec2.DescribeInstancesInput{
			InstanceIds: aws.StringSlice(instanceIDs),
		})
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var instances []*ec2.Instance
	for _, reservation := range output.Reservations {
		instances = append(instances, reservation.Instances...)
	}
	return instances, nil
}

var failedStates = []string{
	ec2.BatchStateCancelled,
	ec2.BatchStateFailed,
	ec2.BatchStateCancelledRunning,
	ec2.BatchStateCancelledTerminating,
}

func WaitForState(ctx context.Context, instanceIDs []string, state string) error {
	for i := 0; i < 300; i++ {
		instances, err := RetryDescribeInstances(ctx, instanceIDs)
		if err != nil {
			return err
		}
		ready := 0
		for _, instance := range instances {
			if *instance.State.Name == state {
				ready++
			}
		}
		Logger.Printf("waiting for state %s %d/%d\n", state, ready, len(instanceIDs))
		if ready == len(instanceIDs) {
			return nil
		}
		time.Sleep(15 * time.Second)
	}
	return fmt.Errorf("failed to wait for %s for instances %v", state, instanceIDs)
}

func TeardownSpotFleet(ctx context.Context, spotFleetRequestId *string) error {
	_, err := EC2Client().CancelSpotFleetRequestsWithContext(ctx, &ec2.CancelSpotFleetRequestsInput{
		SpotFleetRequestIds: []*string{spotFleetRequestId},
		TerminateInstances:  aws.Bool(true),
	})
	if err != nil {
		return err
	}
	instances, err := RetryDescribeSpotFleetInstances(ctx, spotFleetRequestId)
	if err != nil {
		return err
	}
	var ids []string
	for _, instance := range instances {
		ids = append(ids, *instance.InstanceId)
	}
	if len(ids) == 0 {
		return nil
	}
	err = WaitForState(ctx, ids, ec2.InstanceStateNameRunning)
	if err != nil {
		return err
	}
	_, err = EC2Client().TerminateInstancesWithContext(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: aws.StringSlice(ids),
	})
	if err != nil {
		return err
	}
	return nil
}

func WaitForSpotFleet(ctx context.Context, input *ec2.RequestSpotFleetOutput, num int) error {
	for i := 0; i < 300; i++ {
		config, err := RetryDescribeSpotFleet(ctx, input)
		if err != nil {
			return err
		}
		for _, state := range failedStates {
			if state == *config.SpotFleetRequestState {
				return fmt.Errorf("spot fleet request failed with state: %s", state)
			}
		}
		if config.ActivityStatus != nil && *config.ActivityStatus == ec2.ActivityStatusError {
			return fmt.Errorf("spot fleet request failed with errors: %s", *config.SpotFleetRequestId) // TODO aws-ec2-new:_spot_errors()
		}
		num_ready := 0
		instances, err := RetryDescribeSpotFleetInstances(ctx, input.SpotFleetRequestId)
		if err != nil {
			return err
		}
		for range instances {
			num_ready++
		}
		if num_ready < num {
			Logger.Printf("waiting for instances: %d/%d\n", num_ready, num)
			time.Sleep(5 * time.Second)
			continue
		}
		return nil
	}
	return fmt.Errorf("failed to wait for instances")
}

func RequestSpotFleet(ctx context.Context, input *FleetConfig) (*ec2.RequestSpotFleetOutput, error) {
	role, err := IAMClient().GetRoleWithContext(ctx, &iam.GetRoleInput{
		RoleName: aws.String("aws-ec2-spot-fleet-tagging-role"),
	})
	if err != nil {
		return nil, err
	}
	launchSpecs := []*ec2.SpotFleetLaunchSpecification{}
	for _, subnetId := range input.SubnetIds {
		for _, instanceType := range input.InstanceTypes {
			launchSpecs = append(launchSpecs, &ec2.SpotFleetLaunchSpecification{
				ImageId:        aws.String(input.AmiID),
				KeyName:        aws.String(input.Key),
				SubnetId:       aws.String(subnetId),
				InstanceType:   aws.String(instanceType),
				SecurityGroups: []*ec2.GroupIdentifier{{GroupId: aws.String(input.SgID)}},
				BlockDeviceMappings: []*ec2.BlockDeviceMapping{{
					DeviceName: aws.String("/dev/sda1"),
					Ebs: &ec2.EbsBlockDevice{
						DeleteOnTermination: aws.Bool(true),
						Encrypted:           aws.Bool(true),
						VolumeType:          aws.String(ec2.VolumeTypeGp3),
						Iops:                aws.Int64(3000),
						Throughput:          aws.Int64(125),
					},
				}},
				TagSpecifications: []*ec2.SpotFleetTagSpecification{{
					ResourceType: aws.String(ec2.ResourceTypeInstance),
					Tags: []*ec2.Tag{
						{Key: aws.String("Name"), Value: aws.String(input.Name)},
					},
				}},
			})
		}
	}
	spotFleet, err := EC2Client().RequestSpotFleetWithContext(ctx, &ec2.RequestSpotFleetInput{SpotFleetRequestConfig: &ec2.SpotFleetRequestConfigData{
		IamFleetRole:                     role.Role.Arn,
		AllocationStrategy:               aws.String(ec2.AllocationStrategyLowestPrice),
		InstanceInterruptionBehavior:     aws.String(ec2.InstanceInterruptionBehaviorTerminate),
		ReplaceUnhealthyInstances:        aws.Bool(false),
		LaunchSpecifications:             launchSpecs,
		TargetCapacity:                   aws.Int64(int64(input.NumInstances)),
		Type:                             aws.String(ec2.FleetTypeRequest),
		TerminateInstancesWithExpiration: aws.Bool(false),
	}})
	if err != nil {
		return nil, err
	}
	err = WaitForSpotFleet(ctx, spotFleet, input.NumInstances)
	if err != nil {
		err2 := TeardownSpotFleet(ctx, spotFleet.SpotFleetRequestId)
		if err2 != nil {
			return nil, err2
		}
		return nil, err
	}
	return spotFleet, nil
}
