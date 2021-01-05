package lib

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
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

type EC2Tag struct {
	Name  string
	Value string
}

type EC2FleetConfig struct {
	NumInstances int
	Name         string
	SgID         string
	AmiID        string
	UserName     string // instance ssh username
	Key          string
	InstanceType string
	SubnetIds    []string
	Gigs         int
	Init         string
	Tags         []EC2Tag
	Profile      string
}

func EC2RetryDescribeSpotFleet(ctx context.Context, spotFleetRequestId *string) (*ec2.SpotFleetRequestConfig, error) {
	Logger.Println("describe spot fleet", *spotFleetRequestId)
	var output *ec2.DescribeSpotFleetRequestsOutput
	err := Retry(ctx, func() error {
		var err error
		output, err = EC2Client().DescribeSpotFleetRequestsWithContext(ctx, &ec2.DescribeSpotFleetRequestsInput{
			SpotFleetRequestIds: []*string{spotFleetRequestId},
		})
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	if len(output.SpotFleetRequestConfigs) != 1 {
		err = fmt.Errorf("not the right number of configs: %d", len(output.SpotFleetRequestConfigs))
		Logger.Println("error:", err)
		return nil, err
	}
	return output.SpotFleetRequestConfigs[0], nil
}

func EC2RetryDescribeSpotFleetActiveInstances(ctx context.Context, spotFleetRequestId *string) ([]*ec2.ActiveInstance, error) {
	Logger.Println("describe spot fleet instances", *spotFleetRequestId)
	var instances []*ec2.ActiveInstance
	var nextToken *string
	for {
		var output *ec2.DescribeSpotFleetInstancesOutput
		err := Retry(ctx, func() error {
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
			Logger.Println("error:", err)
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

func EC2RetryListInstances(ctx context.Context) ([]*ec2.Instance, error) {
	Logger.Println("list instances")
	var instances []*ec2.Instance
	var nextToken *string
	for {
		var output *ec2.DescribeInstancesOutput
		err := Retry(ctx, func() error {
			var err error
			output, err = EC2Client().DescribeInstancesWithContext(ctx, &ec2.DescribeInstancesInput{
				NextToken: nextToken,
			})
			if err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		for _, reservation := range output.Reservations {
			instances = append(instances, reservation.Instances...)
		}
		if output.NextToken == nil {
			break
		}
		nextToken = output.NextToken
	}
	return instances, nil
}

func EC2RetryDescribeInstances(ctx context.Context, instanceIDs []string) ([]*ec2.Instance, error) {
	Logger.Println("describe instances for", len(instanceIDs), "instanceIDs")
	Assert(len(instanceIDs) < 1000, "cannot list 1000 instances by id")
	var output *ec2.DescribeInstancesOutput
	err := Retry(ctx, func() error {
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
		Logger.Println("error:", err)
		return nil, err
	}
	var instances []*ec2.Instance
	for _, reservation := range output.Reservations {
		instances = append(instances, reservation.Instances...)
	}
	return instances, nil
}

var ec2FailedStates = []string{
	ec2.BatchStateCancelled,
	ec2.BatchStateFailed,
	ec2.BatchStateCancelledRunning,
	ec2.BatchStateCancelledTerminating,
}

func EC2WaitForState(ctx context.Context, instanceIDs []string, state string) error {
	Logger.Println("wait for state", state, "for", len(instanceIDs), "instanceIDs")
	for i := 0; i < 300; i++ {
		instances, err := EC2RetryDescribeInstances(ctx, instanceIDs)
		if err != nil {
			Logger.Println("error:", err)
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
		select {
		case <-time.After(15 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	err := fmt.Errorf("failed to wait for %s for instances %v", state, instanceIDs)
	Logger.Println("error:", err)
	return err
}

func EC2FinalizeSpotFleet(ctx context.Context, spotFleetRequestId *string) error {
	Logger.Println("teardown spot fleet", *spotFleetRequestId)
	_, err := EC2Client().CancelSpotFleetRequestsWithContext(ctx, &ec2.CancelSpotFleetRequestsInput{
		SpotFleetRequestIds: []*string{spotFleetRequestId},
		TerminateInstances:  aws.Bool(false),
	})
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	return nil
}

func EC2TeardownSpotFleet(ctx context.Context, spotFleetRequestId *string) error {
	Logger.Println("teardown spot fleet", *spotFleetRequestId)
	_, err := EC2Client().CancelSpotFleetRequestsWithContext(ctx, &ec2.CancelSpotFleetRequestsInput{
		SpotFleetRequestIds: []*string{spotFleetRequestId},
		TerminateInstances:  aws.Bool(true),
	})
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	instances, err := EC2RetryDescribeSpotFleetActiveInstances(ctx, spotFleetRequestId)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	var ids []string
	for _, instance := range instances {
		ids = append(ids, *instance.InstanceId)
	}
	if len(ids) == 0 {
		return nil
	}
	err = EC2WaitForState(ctx, ids, ec2.InstanceStateNameRunning)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	_, err = EC2Client().TerminateInstancesWithContext(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: aws.StringSlice(ids),
	})
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	return nil
}

func ec2SpotFleetHistoryErrors(ctx context.Context, spotFleetRequestId *string) error {
	t := time.Now().UTC().Add(-24 * time.Hour)
	timestamp := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	output, err := EC2Client().DescribeSpotFleetRequestHistoryWithContext(ctx, &ec2.DescribeSpotFleetRequestHistoryInput{
		EventType:          aws.String(ec2.EventTypeError),
		SpotFleetRequestId: spotFleetRequestId,
		StartTime:          aws.Time(timestamp),
	})
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	var errors []string
	for _, record := range output.HistoryRecords {
		errors = append(errors, *record.EventInformation.EventDescription)
	}
	if len(errors) != 0 {
		err = fmt.Errorf(strings.Join(errors, "\n"))
		Logger.Println("error: spot fleet history error:", err)
		return err
	}
	return nil
}

func EC2WaitForSpotFleet(ctx context.Context, spotFleetRequestId *string, num int) error {
	Logger.Println("wait for spot fleet", *spotFleetRequestId, "with", num, "instances")
	for i := 0; i < 300; i++ {
		config, err := EC2RetryDescribeSpotFleet(ctx, spotFleetRequestId)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		for _, state := range ec2FailedStates {
			if state == *config.SpotFleetRequestState {
				err = fmt.Errorf("spot fleet request failed with state: %s", state)
				Logger.Println("error:", err)
				return err
			}
		}
		err = ec2SpotFleetHistoryErrors(ctx, spotFleetRequestId)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		num_ready := 0
		instances, err := EC2RetryDescribeSpotFleetActiveInstances(ctx, spotFleetRequestId)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		for range instances {
			num_ready++
		}
		if num_ready < num {
			Logger.Printf("waiting for instances: %d/%d\n", num_ready, num)
			select {
			case <-time.After(5 * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}
		return nil
	}
	err := fmt.Errorf("failed to wait for instances")
	Logger.Println("error:", err)
	return err
}

const nvmeInit = `
# pick the first nvme drive which is NOT mounted as / and prepare that as /mnt
set -x
while true; do
    echo 'wait for /dev/nvme*'
    if sudo fdisk -l | grep /dev/nvme &>/dev/null; then
        break
    fi
    sleep 1
done
disk=$(sudo fdisk -l | grep ^Disk | grep nvme | awk '{print $2}' | tr -d : | sort -u | grep -v $(df / | grep /dev | awk '{print $1}' | head -c11) | head -n1)
(
 echo g # Create a new empty GPT partition table
 echo n # Add a new partition
 echo 1 # Partition number
 echo   # First sector (Accept default: 1)
 echo   # Last sector (Accept default: varies)
 echo w # Write changes
) | sudo fdisk $disk
sleep 5
yes | sudo mkfs -t ext4 -E nodiscard ${disk}p1
sudo mkdir -p /mnt
sudo mount -o nodiscard,noatime ${disk}p1 /mnt
sudo chown -R $(whoami):$(whoami) /mnt
echo ${disk}p1 /mnt ext4 nodiscard,noatime 0 1 | sudo tee -a /etc/fstab
set +x
`

func EC2RequestSpotFleet(ctx context.Context, spotStrategy string, input *EC2FleetConfig) ([]*ec2.Instance, error) {
	if !Contains(ec2.AllocationStrategy_Values(), spotStrategy) {
		return nil, fmt.Errorf("invalid spot allocation strategy: %s", spotStrategy)
	}
	role, err := IAMClient().GetRoleWithContext(ctx, &iam.GetRoleInput{
		RoleName: aws.String("aws-ec2-spot-fleet-tagging-role"),
	})
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	for _, instanceType := range []string{"i3", "i3en", "c5d", "m5d", "r5d", "z1d"} {
		if instanceType == strings.Split(input.InstanceType, ".")[0] {
			Logger.Println("add nvme instance store setup to init script")
			input.Init = nvmeInit + input.Init
			break
		}
	}
	if input.Init != "" {
		input.Init = base64.StdEncoding.EncodeToString([]byte(input.Init))
		input.Init = fmt.Sprintf("#!/bin/bash\npath=/tmp/$(uuidgen); echo %s | base64 -d > $path; sudo -u %s bash -e $path 2>&1", input.Init, input.UserName)
		input.Init = base64.StdEncoding.EncodeToString([]byte(input.Init))
	}
	launchSpecs := []*ec2.SpotFleetLaunchSpecification{}
	tags := []*ec2.Tag{
		{Key: aws.String("Name"), Value: aws.String(input.Name)},
		{Key: aws.String("user"), Value: aws.String(input.UserName)},
		{Key: aws.String("creation-date"), Value: aws.String(time.Now().UTC().Format(time.RFC3339))},
	}
	for _, tag := range input.Tags {
		tags = append(tags, &ec2.Tag{
			Key:   aws.String(tag.Name),
			Value: aws.String(tag.Value),
		})
	}
	for _, subnetId := range input.SubnetIds {
		launchSpecs = append(launchSpecs, &ec2.SpotFleetLaunchSpecification{
			ImageId:            aws.String(input.AmiID),
			KeyName:            aws.String(input.Key),
			SubnetId:           aws.String(subnetId),
			InstanceType:       aws.String(input.InstanceType),
			UserData:           aws.String(input.Init),
			IamInstanceProfile: &ec2.IamInstanceProfileSpecification{Name: aws.String(input.Profile)},
			SecurityGroups:     []*ec2.GroupIdentifier{{GroupId: aws.String(input.SgID)}},
			BlockDeviceMappings: []*ec2.BlockDeviceMapping{{
				DeviceName: aws.String("/dev/sda1"),
				Ebs: &ec2.EbsBlockDevice{
					DeleteOnTermination: aws.Bool(true),
					Encrypted:           aws.Bool(true),
					VolumeType:          aws.String(ec2.VolumeTypeGp3),
					Iops:                aws.Int64(3000),
					Throughput:          aws.Int64(125),
					VolumeSize:          aws.Int64(int64(input.Gigs)),
				},
			}},
			TagSpecifications: []*ec2.SpotFleetTagSpecification{{
				ResourceType: aws.String(ec2.ResourceTypeInstance),
				Tags:         tags,
			}},
		})
	}
	Logger.Println("type:", input.InstanceType)
	Logger.Println("subnets:", input.SubnetIds)
	Logger.Println("requst spot fleet", DropLinesWithAny(Pformat(launchSpecs[0]), "null", "SubnetId", "InstanceType"))
	spotFleet, err := EC2Client().RequestSpotFleetWithContext(ctx, &ec2.RequestSpotFleetInput{SpotFleetRequestConfig: &ec2.SpotFleetRequestConfigData{
		IamFleetRole:                     role.Role.Arn,
		LaunchSpecifications:             launchSpecs,
		AllocationStrategy:               aws.String(spotStrategy),
		InstanceInterruptionBehavior:     aws.String(ec2.InstanceInterruptionBehaviorTerminate),
		Type:                             aws.String(ec2.FleetTypeRequest),
		TargetCapacity:                   aws.Int64(int64(input.NumInstances)),
		ReplaceUnhealthyInstances:        aws.Bool(false),
		TerminateInstancesWithExpiration: aws.Bool(false),
	}})
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	err = EC2WaitForSpotFleet(ctx, spotFleet.SpotFleetRequestId, input.NumInstances)
	if err != nil {
		Logger.Println("error:", err)
		err2 := EC2TeardownSpotFleet(context.Background(), spotFleet.SpotFleetRequestId)
		if err2 != nil {
			Logger.Println("error:", err2)
			return nil, err2
		}
		return nil, err
	}
	err = EC2FinalizeSpotFleet(ctx, spotFleet.SpotFleetRequestId)
	if err != nil {
		return nil, err
	}
	var instanceIDs []string
	fleetInstances, err := EC2RetryDescribeSpotFleetActiveInstances(ctx, spotFleet.SpotFleetRequestId)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	for _, instance := range fleetInstances {
		instanceIDs = append(instanceIDs, *instance.InstanceId)
	}
	instances, err := EC2RetryDescribeInstances(ctx, instanceIDs)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	return instances, nil
}
