package lib

import (
	"context"
	"encoding/base64"
	"fmt"
	"golang.org/x/sync/semaphore"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

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

type EC2Config struct {
	NumInstances   int
	Name           string
	SgID           string
	AmiID          string
	UserName       string // instance ssh username
	Key            string
	InstanceType   string
	SubnetIds      []string
	Gigs           int
	Throughput     int
	Iops           int
	Init           string
	Tags           []EC2Tag
	Profile        string
	SecondsTimeout int
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

type Kind string

const (
	KindTags             Kind = "tags"
	KindDnsName          Kind = "dns-name"
	KindVpcId            Kind = "vpc-id"
	KindSubnetID         Kind = "subnet-id"
	KindSecurityGroupID  Kind = "instance.group-id"
	KindPrivateDnsName   Kind = "private-dns-name"
	KindIPAddress        Kind = "ip-address"
	KindPrivateIPAddress Kind = "private-ip-address"
	KindInstanceID       Kind = "instance-id"
)

func isIPAddress(s string) bool {
	for _, c := range s {
		if c != '.' {
			_, err := strconv.Atoi(string(c))
			if err != nil {
				return false
			}
		}
	}
	return true
}

func EC2RetryListInstances(ctx context.Context, selectors []string, state string) ([]*ec2.Instance, error) {
	Logger.Println("list instances")
	var filterss [][]*ec2.Filter
	if len(selectors) == 0 {
		if state != "" {
			filterss = append(filterss, []*ec2.Filter{{Name: aws.String("instance-state-name"), Values: []*string{aws.String(state)}}})
		} else {
			filterss = append(filterss, []*ec2.Filter{{}})
		}
	} else {
		kind := KindTags
		if strings.HasSuffix(selectors[0], ".amazonaws.com") {
			kind = KindDnsName
		} else if strings.HasPrefix(selectors[0], "vpc-") {
			kind = KindVpcId
		} else if strings.HasPrefix(selectors[0], "subnet-") {
			kind = KindSubnetID
		} else if strings.HasPrefix(selectors[0], "sg-") {
			kind = KindSecurityGroupID
		} else if strings.HasSuffix(selectors[0], ".ec2.internal") {
			kind = KindPrivateDnsName
		} else if isIPAddress(selectors[0]) {
			part := strings.Split(selectors[0], ".")[0]
			if part == "10" || part == "172" {
				kind = KindPrivateIPAddress
			} else {
				kind = KindIPAddress
			}
		} else if strings.HasPrefix(selectors[0], "i-") {
			kind = KindInstanceID
		}
		if kind == KindTags && !strings.Contains(selectors[0], "=") {
			selectors[0] = fmt.Sprintf("Name=%s", selectors[0])
		}
		for _, chunk := range Chunk(selectors, 195) { // max aws api params per query is 200
			var filters []*ec2.Filter
			if state != "" {
				filters = append(filters, &ec2.Filter{Name: aws.String("instance-state-name"), Values: []*string{aws.String(state)}})
			}
			for _, selector := range chunk {
				if kind == KindTags {
					parts := strings.Split(selector, "=")
					k := parts[0]
					v := parts[1]
					filters = append(filters, &ec2.Filter{Name: aws.String(fmt.Sprintf("tag:%s", k)), Values: []*string{aws.String(v)}})
				} else {
					filters = append(filters, &ec2.Filter{Name: aws.String(string(kind)), Values: aws.StringSlice(chunk)})
				}
			}
			filterss = append(filterss, filters)
		}

	}
	var instances []*ec2.Instance
	var nextToken *string
	for _, filters := range filterss {
		for {
			var output *ec2.DescribeInstancesOutput
			err := Retry(ctx, func() error {
				var err error
				output, err = EC2Client().DescribeInstancesWithContext(ctx, &ec2.DescribeInstancesInput{
					Filters:   filters,
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
	}
	sort.SliceStable(instances, func(i, j int) bool { return *instances[i].InstanceId < *instances[j].InstanceId })
	sort.SliceStable(instances, func(i, j int) bool { return tag(instances[i], "Name", "") < tag(instances[j], "Name", "") })
	sort.SliceStable(instances, func(i, j int) bool { return instances[i].LaunchTime.UnixNano() > instances[j].LaunchTime.UnixNano() })
	return instances, nil
}

func tags(instance *ec2.Instance) map[string]string {
	val := make(map[string]string)
	for _, tag := range instance.Tags {
		val[*tag.Key] = *tag.Value
	}
	return val
}

func tag(instance *ec2.Instance, key string, defaultValue string) string {
	val, ok := tags(instance)[key]
	if !ok {
		val = defaultValue
	}
	return val
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

const timeoutInit = `
echo '# timeout will call this script before it $(sudo poweroff)s, and wait 60 seconds for this script to complete' | sudo tee -a /etc/timeout.sh
echo '#!/bin/bash
    warning="seconds remaining until timeout poweroff. [sudo journalctl -u timeout.service -f] to follow. increase /etc/timeout.seconds to delay. [date +%s | sudo tee /etc/timeout.start.seconds] to reset, or [sudo systemctl {{stop,disable}} timeout.service] to cancel."
    echo TIMEOUT_SECONDS | sudo tee /etc/timeout.seconds
    # count down until timeout
    if [ ! -f /etc/timeout.true_start.seconds ]; then
        date +%s | sudo tee /etc/timeout.true_start.seconds
    fi
    if [ ! -f /etc/timeout.start.seconds ]; then
        date +%s | sudo tee /etc/timeout.start.seconds
    fi
    while true; do
        start=$(cat /etc/timeout.start.seconds)
        true_start=$(cat /etc/timeout.true_start.seconds)
        now=$(date +%s)
        duration=$(($now - $start))
        true_duration=$(($now - $true_start))
        timeout=$(cat /etc/timeout.seconds)
        if (($duration > $timeout)); then
            break
        fi
        remaining=$(($timeout - $duration))
        if (($remaining <= 300)) && (($remaining % 60 == 0)); then
            wall "$remaining $warning"
        fi
        echo uptime seconds: $true_duration
        echo poweroff in seconds: $remaining
        sleep 5
    done
    # run timeout script and wait 60 seconds
    echo run: bash /etc/timeout.sh
    bash /etc/timeout.sh &
    pid=$!
    start=$(date +%s)
    overtime=60
    while true; do
        ps $pid || break
        now=$(date +%s)
        duration=$(($now - $start))
        (($duration > $overtime)) && break
        remaining=$(($overtime - $duration))
        echo seconds until poweroff: $remaining
        sleep 1
    done
    sudo poweroff # sudo poweroff terminates spot instances by default
' |  sudo tee /usr/local/bin/timeout
sudo chmod +x /usr/local/bin/timeout

echo '[Unit]
Description=timeout

[Service]
Type=simple
ExecStart=/usr/local/bin/timeout
User=root
Restart=always

[Install]
WantedBy=multi-user.target
' | sudo tee /etc/systemd/system/timeout.service

sudo systemctl daemon-reload
sudo systemctl start timeout.service
sudo systemctl enable timeout.service
`

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

func makeBlockDeviceMapping(config *EC2Config) []*ec2.BlockDeviceMapping {
	return []*ec2.BlockDeviceMapping{{
		DeviceName: aws.String("/dev/sda1"),
		Ebs: &ec2.EbsBlockDevice{
			DeleteOnTermination: aws.Bool(true),
			Encrypted:           aws.Bool(true),
			VolumeType:          aws.String(ec2.VolumeTypeGp3),
			Iops:                aws.Int64(int64(config.Iops)),
			Throughput:          aws.Int64(int64(config.Throughput)),
			VolumeSize:          aws.Int64(int64(config.Gigs)),
		},
	}}
}

func EC2RequestSpotFleet(ctx context.Context, spotStrategy string, config *EC2Config) ([]*ec2.Instance, error) {
	config = ec2ConfigDefaults(config)
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
	launchSpecs := []*ec2.SpotFleetLaunchSpecification{}
	for _, subnetId := range config.SubnetIds {
		launchSpecs = append(launchSpecs, &ec2.SpotFleetLaunchSpecification{
			ImageId:             aws.String(config.AmiID),
			KeyName:             aws.String(config.Key),
			SubnetId:            aws.String(subnetId),
			InstanceType:        aws.String(config.InstanceType),
			UserData:            aws.String(makeInit(config)),
			IamInstanceProfile:  &ec2.IamInstanceProfileSpecification{Name: aws.String(config.Profile)},
			SecurityGroups:      []*ec2.GroupIdentifier{{GroupId: aws.String(config.SgID)}},
			BlockDeviceMappings: makeBlockDeviceMapping(config),
			TagSpecifications: []*ec2.SpotFleetTagSpecification{{
				ResourceType: aws.String(ec2.ResourceTypeInstance),
				Tags:         makeTags(config),
			}},
		})
	}
	Logger.Println("type:", config.InstanceType)
	Logger.Println("subnets:", config.SubnetIds)
	Logger.Println("requst spot fleet", DropLinesWithAny(Pformat(launchSpecs[0]), "null", "SubnetId", "UserData"))
	spotFleet, err := EC2Client().RequestSpotFleetWithContext(ctx, &ec2.RequestSpotFleetInput{SpotFleetRequestConfig: &ec2.SpotFleetRequestConfigData{
		IamFleetRole:                     role.Role.Arn,
		LaunchSpecifications:             launchSpecs,
		AllocationStrategy:               aws.String(spotStrategy),
		InstanceInterruptionBehavior:     aws.String(ec2.InstanceInterruptionBehaviorTerminate),
		Type:                             aws.String(ec2.FleetTypeRequest),
		TargetCapacity:                   aws.Int64(int64(config.NumInstances)),
		ReplaceUnhealthyInstances:        aws.Bool(false),
		TerminateInstancesWithExpiration: aws.Bool(false),
	}})
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	err = EC2WaitForSpotFleet(ctx, spotFleet.SpotFleetRequestId, config.NumInstances)
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

func makeInit(config *EC2Config) string {
	init := config.Init
	for _, instanceType := range []string{"i3", "i3en", "c5d", "m5d", "r5d", "z1d"} {
		if instanceType == strings.Split(config.InstanceType, ".")[0] {
			Logger.Println("add nvme instance store setup to init script")
			init = nvmeInit + init
			break
		}
	}
	if config.SecondsTimeout != 0 {
		init = strings.Replace(timeoutInit, "TIMEOUT_SECONDS", fmt.Sprint(config.SecondsTimeout), 1) + init
	}
	if init != "" {
		init = base64.StdEncoding.EncodeToString([]byte(init))
		init = fmt.Sprintf("#!/bin/bash\npath=/tmp/$(uuidgen); echo %s | base64 -d > $path; sudo -u %s bash -e $path 2>&1", init, config.UserName)
		init = base64.StdEncoding.EncodeToString([]byte(init))
	}
	return init
}

func makeTags(config *EC2Config) []*ec2.Tag {
	tags := []*ec2.Tag{
		{Key: aws.String("Name"), Value: aws.String(config.Name)},
		{Key: aws.String("user"), Value: aws.String(config.UserName)},
		{Key: aws.String("creation-date"), Value: aws.String(time.Now().UTC().Format(time.RFC3339))},
	}
	for _, tag := range config.Tags {
		tags = append(tags, &ec2.Tag{
			Key:   aws.String(tag.Name),
			Value: aws.String(tag.Value),
		})
	}
	return tags
}

func ec2ConfigDefaults(config *EC2Config) *EC2Config {
	if config.Iops == 0 {
		config.Iops = 3000
	}
	if config.Throughput == 0 {
		config.Throughput = 125
	}
	if config.Gigs == 0 {
		config.Gigs = 16
	}
	return config
}

func EC2NewInstances(ctx context.Context, config *EC2Config) ([]*ec2.Instance, error) {
	config = ec2ConfigDefaults(config)
	if len(config.SubnetIds) != 1 {
		err := fmt.Errorf("must specify exactly one subnet, got: %v", config.SubnetIds)
		Logger.Println("error:", err)
		return nil, err
	}
	runInstancesInput := &ec2.RunInstancesInput{
		ImageId:             aws.String(config.AmiID),
		KeyName:             aws.String(config.Key),
		SubnetId:            aws.String(config.SubnetIds[0]),
		InstanceType:        aws.String(config.InstanceType),
		UserData:            aws.String(makeInit(config)),
		IamInstanceProfile:  &ec2.IamInstanceProfileSpecification{Name: aws.String(config.Profile)},
		SecurityGroupIds:    []*string{&config.SgID},
		BlockDeviceMappings: makeBlockDeviceMapping(config),
		MinCount:            aws.Int64(int64(config.NumInstances)),
		MaxCount:            aws.Int64(int64(config.NumInstances)),
		TagSpecifications: []*ec2.TagSpecification{{
			ResourceType: aws.String(ec2.ResourceTypeInstance),
			Tags:         makeTags(config),
		}},
	}
	Logger.Println("run instances", DropLinesWithAny(Pformat(runInstancesInput), "null", "UserData"))
	reservation, err := EC2Client().RunInstancesWithContext(ctx, runInstancesInput)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	return reservation.Instances, nil
}

type EC2SshInput struct {
	NoTTY          bool
	Instances      []*ec2.Instance
	Cmd            string
	TimeoutSeconds int
	MaxConcurrency int
	User           string
	Stdout         io.WriteCloser
	Stderr         io.WriteCloser
	Stdin          string
}

func publicKey(path string) (ssh.AuthMethod, error) {
	key, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, err
	}
	return ssh.PublicKeys(signer), nil
}

const remoteCmdTemplate = `
fail_msg="failed to run cmd on instance: %s"
mkdir -p ~/.cmds || echo $fail_msg
path=~/.cmds/$(uuidgen)
input=$path.input
echo %s | base64 -d > $path  || echo $fail_msg
echo %s | base64 -d > $input || echo $fail_msg
cat $input | bash $path
code=$?
if [ $code != 0 ]; then
    echo $fail_msg
    exit $code
fi
`

func remoteCmd(cmd, stdin, instanceID string) string {
	return fmt.Sprintf(
		remoteCmdTemplate,
		instanceID,
		base64.StdEncoding.EncodeToString([]byte(cmd)),
		base64.StdEncoding.EncodeToString([]byte(stdin)),
	)
}

func EC2Ssh(ctx context.Context, input *EC2SshInput) error {
	if len(input.Instances) == 0 {
		err := fmt.Errorf("no instances")
		Logger.Println("error:", err)
		return err
	}
	if input.User == "" {
		input.User = tag(input.Instances[0], "user", "")
		for _, instance := range input.Instances[1:] {
			user := tag(instance, "user", "")
			if input.User != user {
				err := fmt.Errorf("not all instance users are the same, want: %s, got: %s", input.User, user)
				Logger.Println("error:", err)
				return err
			}
		}
		input.User = tag(input.Instances[0], "user", "")
		if input.User == "" {
			err := fmt.Errorf("no user provied and no user tag available")
			Logger.Println("error:", err)
			return err
		}
	}
	//
	auth := []ssh.AuthMethod{}
	//
	socket := os.Getenv("SSH_AUTH_SOCK")
	agentConn, err := net.Dial("unix", socket)
	if err == nil {
		defer func() { _ = agentConn.Close() }()
		agentClient := agent.NewClient(agentConn)
		auth = append(auth, ssh.PublicKeysCallback(agentClient.Signers))
	}
	//
	id_rsa := fmt.Sprintf("%s/.ssh/id_rsa", os.Getenv("HOME"))
	if Exists(id_rsa) {
		key, err := publicKey(id_rsa)
		if err == nil {
			auth = append(auth, key)
		}
	}
	//
	id_ed25519 := fmt.Sprintf("%s/.ssh/id_ed25519", os.Getenv("HOME"))
	if Exists(id_rsa) {
		key, err := publicKey(id_ed25519)
		if err == nil {
			auth = append(auth, key)
		}
	}
	if len(auth) == 0 {
		err := fmt.Errorf("ssh agent not running and no keys found in ~/.ssh")
		Logger.Println("error:", err)
		return err
	}
	//
	config := &ssh.ClientConfig{
		User:            input.User,
		Auth:            auth,
		Timeout:         5 * time.Second,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	//
	if input.MaxConcurrency == 0 {
		input.MaxConcurrency = 32
	}
	if input.TimeoutSeconds != 0 {
		timeoutCtx, timeoutCancel := context.WithTimeout(ctx, time.Duration(input.TimeoutSeconds)*time.Second)
		defer timeoutCancel()
		ctx = timeoutCtx
	}
	done := make(chan error, len(input.Instances))
	concurrency := semaphore.NewWeighted(int64(input.MaxConcurrency))
	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	for _, instance := range input.Instances {
		go func(instance *ec2.Instance) {
			err := concurrency.Acquire(cancelCtx, 1)
			if err != nil {
				done <- err
				return
			}
			defer concurrency.Release(1)
			done <- ec2Ssh(cancelCtx, config, instance, input)
		}(instance)
	}
	var errLast error
	for range input.Instances {
		err := <-done
		if err != nil {
			cancel()
			errLast = err
		}
	}
	//
	return errLast
}

func sshDialContext(ctx context.Context, network, addr string, config *ssh.ClientConfig) (*ssh.Client, error) {
	var d net.Dialer
	dialContext, dialCancel := context.WithTimeout(ctx, config.Timeout)
	defer dialCancel()
	conn, err := d.DialContext(dialContext, network, addr)
	if err != nil {
		return nil, err
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		return nil, err
	}
	return ssh.NewClient(c, chans, reqs), nil
}

func ec2Ssh(ctx context.Context, config *ssh.ClientConfig, instance *ec2.Instance, input *EC2SshInput) error {
	sshConn, err := sshDialContext(ctx, "tcp", fmt.Sprintf("%s:22", *instance.PublicDnsName), config)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	defer func() { _ = sshConn.Close() }()
	//
	sshSession, err := sshConn.NewSession()
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	defer func() { _ = sshSession.Close() }()
	//
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	sshSession.Stdout = input.Stdout
	sshSession.Stderr = input.Stderr
	//
	if !input.NoTTY {
		err = sshSession.RequestPty("xterm", 80, 24, ssh.TerminalModes{})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	cmd := remoteCmd(input.Cmd, input.Stdin, *instance.InstanceId)
	runContext, runCancel := context.WithCancel(ctx)
	defer runCancel()
	go func() {
		<-runContext.Done()
		_ = sshSession.Close()
		_ = sshConn.Close()
	}()
	err = sshSession.Run(cmd)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	return nil
}

func EC2SshLogin(instance *ec2.Instance, user string) error {
	if user == "" {
		user = tag(instance, "user", "")
		if user == "" {
			err := fmt.Errorf("no user provied and no user tag available")
			Logger.Println("error:", err)
			return err
		}
	}
	cmd := exec.Command(
		"ssh",
		user+"@"+*instance.PublicDnsName,
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "StrictHostKeyChecking=no",
	)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	return nil
}

func ec2AmiLambda(ctx context.Context) (string, error) {
	resp, err := http.Get("https://docs.aws.amazon.com/lambda/latest/dg/current-supported-versions.html")
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("bad http status code: %d", resp.StatusCode)
		Logger.Println("error:", err)
		return "", err
	}
	r := regexp.MustCompile("(amzn-ami-hvm[^\" ]+)")
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	ami := r.FindAllString(string(body), -1)[0]
	out, err := EC2Client().DescribeImagesWithContext(ctx, &ec2.DescribeImagesInput{
		Owners:  []*string{aws.String("137112412989")},
		Filters: []*ec2.Filter{{Name: aws.String("name"), Values: []*string{aws.String(ami)}}},
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	if len(out.Images) != 1 {
		err := fmt.Errorf("didn't find ami for: %s [%d]", ami, len(out.Images))
		Logger.Println("error:", err)
		return "", err
	}
	fmt.Println(*out.Images[0].OwnerId)
	return *out.Images[0].ImageId, nil
}

var ubuntus = map[string]string{
	"focal":  "ubuntu/images/hvm-ssd/ubuntu-focal-20.04-amd64-server",
	"bionic": "ubuntu/images/hvm-ssd/ubuntu-bionic-18.04-amd64-server",
	"xenial": "ubuntu/images/hvm-ssd/ubuntu-xenial-16.04-amd64-server",
	"trusty": "ubuntu/images/hvm-ssd/ubuntu-trusty-14.04-amd64-server",
}

func keys(m map[string]string) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

func ec2AmiUbuntu(ctx context.Context, name string) (string, error) {
	fragment, ok := ubuntus[name]
	if !ok {
		err := fmt.Errorf("bad ubuntu name %s, should be one of: %v", name, keys(ubuntus))
		Logger.Println("error:", err)
		return "", err
	}
	out, err := EC2Client().DescribeImagesWithContext(ctx, &ec2.DescribeImagesInput{
		Owners: []*string{aws.String("099720109477")},
		Filters: []*ec2.Filter{
			{Name: aws.String("name"), Values: []*string{aws.String(fmt.Sprintf("*%s*", fragment))}},
			{Name: aws.String("architecture"), Values: []*string{aws.String("x86_64")}},
		},
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	sort.Slice(out.Images, func(i, j int) bool { return *out.Images[i].CreationDate > *out.Images[j].CreationDate })
	return *out.Images[0].ImageId, nil
}

func ec2AmiAmzn(ctx context.Context) (string, error) {
	out, err := EC2Client().DescribeImagesWithContext(ctx, &ec2.DescribeImagesInput{
		Owners: []*string{aws.String("137112412989")},
		Filters: []*ec2.Filter{
			{Name: aws.String("name"), Values: []*string{aws.String("amzn2-ami-hvm-2.0*-ebs")}},
			{Name: aws.String("architecture"), Values: []*string{aws.String("x86_64")}},
		},
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	sort.Slice(out.Images, func(i, j int) bool { return *out.Images[i].CreationDate > *out.Images[j].CreationDate })
	return *out.Images[0].ImageId, nil
}

func ec2AmiArch(ctx context.Context) (string, error) {
	out, err := EC2Client().DescribeImagesWithContext(ctx, &ec2.DescribeImagesInput{
		Owners: []*string{aws.String("093273469852")},
		Filters: []*ec2.Filter{
			{Name: aws.String("name"), Values: []*string{aws.String("arch-linux-hvm-*-ebs")}},
			{Name: aws.String("architecture"), Values: []*string{aws.String("x86_64")}},
		},
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	sort.Slice(out.Images, func(i, j int) bool { return *out.Images[i].CreationDate > *out.Images[j].CreationDate })
	return *out.Images[0].ImageId, nil
}

func EC2Ami(ctx context.Context, name string) (amiID string, sshUser string, err error) {
	switch name {
	case "lambda":
		amiID, err = ec2AmiLambda(ctx)
		sshUser = "user"
	case "amzn":
		amiID, err = ec2AmiAmzn(ctx)
		sshUser = "user"
	case "arch":
		amiID, err = ec2AmiArch(ctx)
		sshUser = "arch"
	default:
		amiID, err = ec2AmiUbuntu(ctx, name)
		sshUser = "ubuntu"
	}
	return amiID, sshUser, err
}

func EC2ZonesWithInstance(ctx context.Context, instanceType string) (zones []string, err error) {
	out, err := EC2Client().DescribeInstanceTypeOfferingsWithContext(ctx, &ec2.DescribeInstanceTypeOfferingsInput{
		LocationType: aws.String("availability-zone"),
		Filters:      []*ec2.Filter{{Name: aws.String("instance-type"), Values: []*string{aws.String(instanceType)}}},
	})
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	for _, offer := range out.InstanceTypeOfferings {
		zones = append(zones, *offer.Location)
	}
	return zones, nil
}

func EC2SgID(ctx context.Context, name string) (string, error) {
	out, err := EC2Client().DescribeSecurityGroupsWithContext(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []*ec2.Filter{{Name: aws.String("tag:Name"), Values: []*string{aws.String(name)}}},
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	if len(out.SecurityGroups) != 1 {
		err = fmt.Errorf("didn't find exactly 1 security group for name: %s", name)
		Logger.Println("error:", err)
		return "", err
	}
	return *out.SecurityGroups[0].GroupId, nil
}
