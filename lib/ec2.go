package lib

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
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
	"golang.org/x/sync/semaphore"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/iam"
)

const (
	EC2ArchAmd64 = "x86_64"
	EC2ArchArm64 = "arm64"

	EC2AmiLambda = "lambda"
	EC2AmiAmzn   = "amzn"
	EC2AmiArch   = "arch"
	EC2AmiAlpine = "alpine"

	EC2AmiUbuntuFocal  = "focal"
	EC2AmiUbuntuBionic = "bionic"
	EC2AmiUbuntuXenial = "xenial"
	EC2AmiUbuntuTrusty = "trusty"
)

var ec2Client *ec2.EC2
var ec2ClientLock sync.RWMutex

func EC2ClientExplicit(accessKeyID, accessKeySecret, region string) *ec2.EC2 {
	return ec2.New(SessionExplicit(accessKeyID, accessKeySecret, region))
}

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

func EC2DescribeSpotFleet(ctx context.Context, spotFleetRequestId *string) (*ec2.SpotFleetRequestConfig, error) {
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

func EC2DescribeSpotFleetActiveInstances(ctx context.Context, spotFleetRequestId *string) ([]*ec2.ActiveInstance, error) {
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

func EC2ListInstances(ctx context.Context, selectors []string, state string) ([]*ec2.Instance, error) {
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
	sort.SliceStable(instances, func(i, j int) bool {
		return EC2GetTag(instances[i].Tags, "Name", "") < EC2GetTag(instances[j].Tags, "Name", "")
	})
	sort.SliceStable(instances, func(i, j int) bool { return instances[i].LaunchTime.UnixNano() > instances[j].LaunchTime.UnixNano() })
	return instances, nil
}

func ec2Tags(tags []*ec2.Tag) map[string]string {
	val := make(map[string]string)
	for _, tag := range tags {
		val[*tag.Key] = *tag.Value
	}
	return val
}

func EC2GetTag(tags []*ec2.Tag, key string, defaultValue string) string {
	val, ok := ec2Tags(tags)[key]
	if !ok {
		val = defaultValue
	}
	return val
}

func EC2DescribeInstances(ctx context.Context, instanceIDs []string) ([]*ec2.Instance, error) {
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
		instances, err := EC2DescribeInstances(ctx, instanceIDs)
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
	instances, err := EC2DescribeSpotFleetActiveInstances(ctx, spotFleetRequestId)
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
		config, err := EC2DescribeSpotFleet(ctx, spotFleetRequestId)
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
		instances, err := EC2DescribeSpotFleetActiveInstances(ctx, spotFleetRequestId)
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
echo '# timeout will call this script before it $(sudo poweroff)s, and wait 60 seconds for this script to complete' | sudo tee -a /etc/timeout.sh >/dev/null
echo '#!/bin/bash
    warning="seconds remaining until timeout poweroff. [sudo journalctl -u timeout.service -f] to follow. increase /etc/timeout.seconds to delay. [date +%s | sudo tee /etc/timeout.start.seconds] to reset, or [sudo systemctl {{stop,disable}} timeout.service] to cancel."
    echo TIMEOUT_SECONDS | sudo tee /etc/timeout.seconds >/dev/null
    # count down until timeout
    if [ ! -f /etc/timeout.true_start.seconds ]; then
        date +%s | sudo tee /etc/timeout.true_start.seconds >/dev/null
    fi
    if [ ! -f /etc/timeout.start.seconds ]; then
        date +%s | sudo tee /etc/timeout.start.seconds >/dev/null
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
        if (($remaining <= 300)) && (($remaining % 60 == 0)) && which wall >/dev/null; then
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
        ps | awk '{print $1}' | grep $pid || break
        now=$(date +%s)
        duration=$(($now - $start))
        (($duration > $overtime)) && break
        remaining=$(($overtime - $duration))
        echo seconds until poweroff: $remaining
        sleep 1
    done
    date +%s | sudo tee /etc/timeout.start.seconds >/dev/null # reset start time so when we power the machine back up, the timer is reset
    sudo poweroff # sudo poweroff terminates spot instances by default
' |  sudo tee /usr/local/bin/ec2-timeout >/dev/null
sudo chmod +x /usr/local/bin/ec2-timeout

if which systemctl; then
    echo '[Unit]
Description=ec2-timeout

[Service]
Type=simple
ExecStart=/usr/local/bin/ec2-timeout
User=root
Restart=always

[Install]
WantedBy=multi-user.target
' | sudo tee /etc/systemd/system/timeout.service >/dev/null

    sudo systemctl daemon-reload
    sudo systemctl start timeout.service
    sudo systemctl enable timeout.service

elif which rc-update; then

    echo '#!/sbin/openrc-run

command="/usr/local/bin/ec2-timeout"
command_background="yes"
pidfile="/tmp/timeout.pid"
output_log="/var/log/timeout.log"
error_log="/var/log/timeout.log"
' | sudo tee /etc/init.d/timeout >/dev/null
    sudo touch /var/log/timeout.log
    sudo chmod ugo+rw /var/log/timeout.log
    sudo chmod +x /etc/init.d/timeout
    sudo rc-update add timeout default
    sudo rc-service timeout start
else
    sudo touch /var/log/timeout.log
    sudo chmod ugo+rw /var/log/timeout.log
    nohup /usr/local/bin/ec2-timeout >/var/log/timeout.log &
fi

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

# alpine arm64 is different from not alpine x86_64, not sure about alpine x86_64
if which apk &>/dev/null; then
    sudo apk add e2fsprogs
    disk=$(sudo fdisk -l | grep ^Disk | grep nvme | awk '{print $2}' | tr -d : | sort -u | grep -v $(df / | grep /dev | awk '{print $1}' | head -c11) | tail -n1)
    (
     echo n # Add a new partition
     echo p # Primary partition
     echo 1 # Partition number
     echo   # First sector (Accept default: 1)
     echo   # Last sector (Accept default: varies)
     echo w # Write changes
    ) | sudo fdisk $disk
else
    disk=$(sudo fdisk -l | grep ^Disk | grep nvme | awk '{print $2}' | tr -d : | sort -u | grep -v $(df / | grep /dev | awk '{print $1}' | head -c11) | tail -n1)
    (
     echo g # Create a new empty GPT partition table
     echo n # Add a new partition
     echo 1 # Partition number
     echo   # First sector (Accept default: 1)
     echo   # Last sector (Accept default: varies)
     echo w # Write changes
    ) | sudo fdisk $disk
fi

sleep 5
yes | sudo mkfs.ext4 -E nodiscard ${disk}p1
sudo mkdir -p /mnt
sudo mount -o nodiscard,noatime ${disk}p1 /mnt
sudo chown -R $(whoami):$(whoami) /mnt
echo ${disk}p1 /mnt ext4 nodiscard,noatime 0 1 | sudo tee -a /etc/fstab
set +x
`

func makeBlockDeviceMapping(config *EC2Config) []*ec2.BlockDeviceMapping {
	deviceName := "/dev/sda1"
	if config.UserName == "alpine" {
		deviceName = "/dev/xvda"
	}
	return []*ec2.BlockDeviceMapping{{
		DeviceName: aws.String(deviceName),
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
	if config.UserName == "" {
		err := fmt.Errorf("user name is required %v", *config)
		Logger.Println("error:", err)
		return nil, err
	}
	config = ec2ConfigDefaults(config)
	if !Contains(ec2.AllocationStrategy_Values(), spotStrategy) {
		return nil, fmt.Errorf("invalid spot allocation strategy: %s", spotStrategy)
	}
	role, err := IamClient().GetRoleWithContext(ctx, &iam.GetRoleInput{
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
			EbsOptimized:        aws.Bool(true),
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
	fleetInstances, err := EC2DescribeSpotFleetActiveInstances(ctx, spotFleet.SpotFleetRequestId)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	for _, instance := range fleetInstances {
		instanceIDs = append(instanceIDs, *instance.InstanceId)
	}
	instances, err := EC2DescribeInstances(ctx, instanceIDs)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	return instances, nil
}

func makeInit(config *EC2Config) string {
	if config.UserName == "" {
		panic("makeInit needs a username")
	}
	init := config.Init
	for _, instanceType := range []string{"i3", "i3en", "c5d", "m5d", "r5d", "z1d", "c6gd", "m6gd", "r6gd", "c5ad", "is4gen", "im4gn"} {
		if instanceType == strings.Split(config.InstanceType, ".")[0] {
			Logger.Println("add nvme instance store setup to init script")
			init = nvmeInit + init
			break
		}
	}
	if config.SecondsTimeout != 0 {
		init = strings.Replace(timeoutInit, "TIMEOUT_SECONDS", fmt.Sprint(config.SecondsTimeout), 1) + init
	}
	init = base64.StdEncoding.EncodeToString([]byte(init))
	init = fmt.Sprintf("#!/bin/sh\nset -x; path=/tmp/$(cat /proc/sys/kernel/random/uuid); if which apk >/dev/null; then apk update && apk add curl git procps ncurses-terminfo coreutils sed grep less vim sudo bash && echo -e '%s ALL=(ALL) NOPASSWD:ALL\nroot ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers; fi; echo %s | base64 -d > $path; cd /home/%s; sudo -u %s bash -e $path 2>&1", config.UserName, init, config.UserName, config.UserName)
	init = base64.StdEncoding.EncodeToString([]byte(init))
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
	if config.UserName == "" {
		err := fmt.Errorf("user name is required %v", *config)
		Logger.Println("error:", err)
		return nil, err
	}
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
		EbsOptimized:        aws.Bool(true),
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

type EC2RsyncInput struct {
	Source           string
	Destination      string
	Instances        []*ec2.Instance
	TimeoutSeconds   int
	MaxConcurrency   int
	User             string
	PrivateIP        bool
	Key              string
	PrintLock        sync.RWMutex
	AccumulateResult bool
}

func EC2Rsync(ctx context.Context, input *EC2RsyncInput) ([]*ec2SshResult, error) {
	if len(input.Instances) == 0 {
		err := fmt.Errorf("no instances")
		Logger.Println("error:", err)
		return nil, err
	}
	if input.User == "" {
		input.User = EC2GetTag(input.Instances[0].Tags, "user", "")
		for _, instance := range input.Instances[1:] {
			user := EC2GetTag(instance.Tags, "user", "")
			if input.User != user {
				err := fmt.Errorf("not all instance users are the same, want: %s, got: %s", input.User, user)
				Logger.Println("error:", err)
				return nil, err
			}
		}
		input.User = EC2GetTag(input.Instances[0].Tags, "user", "")
		if input.User == "" {
			err := fmt.Errorf("no user provied and no user tag available")
			Logger.Println("error:", err)
			return nil, err
		}
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
	//
	resultChan := make(chan *ec2SshResult, len(input.Instances))
	concurrency := semaphore.NewWeighted(int64(input.MaxConcurrency))
	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	for _, instance := range input.Instances {
		go func(instance *ec2.Instance) {
			err := concurrency.Acquire(cancelCtx, 1)
			if err != nil {
				resultChan <- &ec2SshResult{Err: err, InstanceID: *instance.InstanceId}
				return
			}
			defer concurrency.Release(1)
			resultChan <- ec2Rsync(cancelCtx, instance, input)
		}(instance)
	}
	var errLast error
	var result []*ec2SshResult
	for range input.Instances {
		sshResult := <-resultChan
		if sshResult.Err != nil {
			Logger.Println("error:", sshResult.Err)
			errLast = sshResult.Err
		}
		result = append(result, sshResult)
	}
	//
	return result, errLast
}

func ec2Rsync(ctx context.Context, instance *ec2.Instance, input *EC2RsyncInput) *ec2SshResult {
	result := &ec2SshResult{
		InstanceID: *instance.InstanceId,
	}
	rsyncCmd := []string{
		"rsync",
		"-avh",
		"--delete",
	}
	if input.Key != "" {
		rsyncCmd = append(rsyncCmd, []string{"-e", fmt.Sprintf("ssh -i %s -o IdentitiesOnly=yes -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no", input.Key)}...)
	} else {
		rsyncCmd = append(rsyncCmd, []string{"-e", "ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no"}...)
	}
	if os.Getenv("RSYNC_OPTIONS") != "" {
		rsyncCmd = append(rsyncCmd, strings.Split(os.Getenv("RSYNC_OPTIONS"), " ")...)
	}
	target := input.User + "@"
	if input.PrivateIP {
		target += *instance.PrivateIpAddress
	} else {
		target += *instance.PublicDnsName
	}
	//
	source := input.Source
	destination := input.Destination
	if strings.HasPrefix(source, ":") {
		source = target + source
	} else if strings.HasPrefix(destination, ":") {
		destination = target + destination
	} else {
		result.Err = fmt.Errorf("neither source nor destination contains ':'")
		Logger.Println("error:", result.Err)
		return result
	}
	//
	for _, src := range strings.Split(source, " ") {
		src = strings.Trim(src, " ")
		if src != "" {
			rsyncCmd = append(rsyncCmd, src)
		}
	}
	for _, dst := range strings.Split(destination, " ") {
		dst = strings.Trim(dst, " ")
		if dst != "" {
			rsyncCmd = append(rsyncCmd, dst)
		}
	}
	//
	cmd := exec.CommandContext(ctx, rsyncCmd[0], rsyncCmd[1:]...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		Logger.Println("error:", err)
		result.Err = err
		return result
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		Logger.Println("error:", err)
		result.Err = err
		return result
	}
	//
	resultLock := &sync.RWMutex{}
	done := make(chan error)
	tail := func(kind string, buf *bufio.Reader) {
		for {
			line, err := buf.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					done <- err
				} else {
					done <- nil
				}
				return
			}
			if strings.Contains(line, "Permission denied (publickey)") {
				done <- fmt.Errorf("ec2 Permission denied (publickey)")
				return
			}
			if len(input.Instances) > 1 {
				line = *instance.InstanceId + ": " + line
			}
			if kind == "stdout" && input.AccumulateResult {
				resultLock.Lock()
				result.Stdout = append(result.Stdout, line)
				resultLock.Unlock()
			}
			input.PrintLock.Lock()
			switch kind {
			case "stderr":
				_, _ = fmt.Fprint(os.Stderr, line)
			case "stdout":
				_, _ = fmt.Fprint(os.Stdout, line)
			default:
				panic("unknown kind: " + kind)
			}
			input.PrintLock.Unlock()
		}
	}
	//
	go tail("stdout", bufio.NewReader(stdout))
	go tail("stderr", bufio.NewReader(stderr))
	err = cmd.Start()
	if err != nil {
		Logger.Println("error:", err)
		result.Err = err
		return result
	}
	for i := 0; i < 2; i++ {
		err := <-done
		if err != nil {
			Logger.Println("error:", err)
			result.Err = err
			return result
		}
	}
	err = cmd.Wait()
	if err != nil {
		Logger.Println("error:", err)
		result.Err = err
		return result
	}
	return result
}

type EC2ScpInput struct {
	Source           string
	Destination      string
	Instances        []*ec2.Instance
	TimeoutSeconds   int
	MaxConcurrency   int
	User             string
	PrivateIP        bool
	Key              string
	PrintLock        sync.RWMutex
	AccumulateResult bool
}

func EC2Scp(ctx context.Context, input *EC2ScpInput) ([]*ec2SshResult, error) {
	if len(input.Instances) == 0 {
		err := fmt.Errorf("no instances")
		Logger.Println("error:", err)
		return nil, err
	}
	if input.User == "" {
		input.User = EC2GetTag(input.Instances[0].Tags, "user", "")
		for _, instance := range input.Instances[1:] {
			user := EC2GetTag(instance.Tags, "user", "")
			if input.User != user {
				err := fmt.Errorf("not all instance users are the same, want: %s, got: %s", input.User, user)
				Logger.Println("error:", err)
				return nil, err
			}
		}
		input.User = EC2GetTag(input.Instances[0].Tags, "user", "")
		if input.User == "" {
			err := fmt.Errorf("no user provied and no user tag available")
			Logger.Println("error:", err)
			return nil, err
		}
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
	//
	resultChan := make(chan *ec2SshResult, len(input.Instances))
	concurrency := semaphore.NewWeighted(int64(input.MaxConcurrency))
	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	for _, instance := range input.Instances {
		go func(instance *ec2.Instance) {
			err := concurrency.Acquire(cancelCtx, 1)
			if err != nil {
				resultChan <- &ec2SshResult{Err: err, InstanceID: *instance.InstanceId}
				return
			}
			defer concurrency.Release(1)
			resultChan <- ec2Scp(cancelCtx, instance, input)
		}(instance)
	}
	var errLast error
	var result []*ec2SshResult
	for range input.Instances {
		sshResult := <-resultChan
		if sshResult.Err != nil {
			Logger.Println("error:", sshResult.Err)
			errLast = sshResult.Err
		}
		result = append(result, sshResult)
	}
	//
	return result, errLast
}

func ec2Scp(ctx context.Context, instance *ec2.Instance, input *EC2ScpInput) *ec2SshResult {
	result := &ec2SshResult{
		InstanceID: *instance.InstanceId,
	}
	scpCmd := []string{
		"scp",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "StrictHostKeyChecking=no",
	}
	if input.Key != "" {
		scpCmd = append(scpCmd, []string{"-o", "IdentitiesOnly=yes", "-i", input.Key}...)
	}
	target := input.User + "@"
	if input.PrivateIP {
		target += *instance.PrivateIpAddress
	} else {
		target += *instance.PublicDnsName
	}
	//
	source := input.Source
	destination := input.Destination
	if strings.HasPrefix(source, ":") {
		source = target + source
	} else if strings.HasPrefix(destination, ":") {
		destination = target + destination
	} else {
		result.Err = fmt.Errorf("neither source nor destination contains ':'")
		Logger.Println("error:", result.Err)
		return result
	}
	scpCmd = append(scpCmd, []string{source, destination}...)
	//
	cmd := exec.CommandContext(ctx, scpCmd[0], scpCmd[1:]...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		Logger.Println("error:", err)
		result.Err = err
		return result
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		Logger.Println("error:", err)
		result.Err = err
		return result
	}
	//
	resultLock := &sync.RWMutex{}
	done := make(chan error)
	tail := func(kind string, buf *bufio.Reader) {
		for {
			line, err := buf.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					done <- err
				} else {
					done <- nil
				}
				return
			}
			if strings.Contains(line, "Permission denied (publickey)") {
				done <- fmt.Errorf("ec2 Permission denied (publickey)")
				return
			}
			if len(input.Instances) > 1 {
				line = *instance.InstanceId + ": " + line
			}
			if kind == "stdout" && input.AccumulateResult {
				resultLock.Lock()
				result.Stdout = append(result.Stdout, line)
				resultLock.Unlock()
			}
			input.PrintLock.Lock()
			switch kind {
			case "stderr":
				_, _ = fmt.Fprint(os.Stderr, line)
			case "stdout":
				_, _ = fmt.Fprint(os.Stdout, line)
			default:
				panic("unknown kind: " + kind)
			}
			input.PrintLock.Unlock()
		}
	}
	//
	go tail("stdout", bufio.NewReader(stdout))
	go tail("stderr", bufio.NewReader(stderr))
	err = cmd.Start()
	if err != nil {
		Logger.Println("error:", err)
		result.Err = err
		return result
	}
	for i := 0; i < 2; i++ {
		err := <-done
		if err != nil {
			Logger.Println("error:", err)
			result.Err = err
			return result
		}
	}
	err = cmd.Wait()
	if err != nil {
		Logger.Println("error:", err)
		result.Err = err
		return result
	}
	return result
}

type EC2SshInput struct {
	Instances        []*ec2.Instance
	Cmd              string
	TimeoutSeconds   int
	MaxConcurrency   int
	User             string
	Stdin            string
	PrivateIP        bool
	Key              string
	AccumulateResult bool
	PrintLock        sync.RWMutex
	NoPrint          bool
}

const remoteCmdTemplate = `
fail_msg="%s"
mkdir -p /dev/shm/.cmds || echo $fail_msg
path=/dev/shm/.cmds/$(cat /proc/sys/kernel/random/uuid)
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

func ec2SshRemoteCmd(cmd, stdin, failureMessage string) string {
	return fmt.Sprintf(
		remoteCmdTemplate,
		failureMessage,
		base64.StdEncoding.EncodeToString([]byte(cmd)),
		base64.StdEncoding.EncodeToString([]byte(stdin)),
	)
}

func EC2Ssh(ctx context.Context, input *EC2SshInput) ([]*ec2SshResult, error) {
	if len(input.Instances) == 0 {
		err := fmt.Errorf("no instances")
		Logger.Println("error:", err)
		return nil, err
	}
	if input.User == "" {
		input.User = EC2GetTag(input.Instances[0].Tags, "user", "")
		for _, instance := range input.Instances[1:] {
			user := EC2GetTag(instance.Tags, "user", "")
			if input.User != user {
				err := fmt.Errorf("not all instance users are the same, want: %s, got: %s", input.User, user)
				Logger.Println("error:", err)
				return nil, err
			}
		}
		input.User = EC2GetTag(input.Instances[0].Tags, "user", "")
		if input.User == "" {
			err := fmt.Errorf("no user provied and no user tag available")
			Logger.Println("error:", err)
			return nil, err
		}
	}
	//
	if !strings.HasPrefix(input.Cmd, "#!") && !strings.HasPrefix(input.Cmd, "set ") {
		input.Cmd = "#!/bin/bash\nset -eou pipefail\n" + input.Cmd
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
	//
	resultChan := make(chan *ec2SshResult, len(input.Instances))
	concurrency := semaphore.NewWeighted(int64(input.MaxConcurrency))
	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	for _, instance := range input.Instances {
		go func(instance *ec2.Instance) {
			err := concurrency.Acquire(cancelCtx, 1)
			if err != nil {
				resultChan <- &ec2SshResult{Err: err, InstanceID: *instance.InstanceId}
				return
			}
			defer concurrency.Release(1)
			resultChan <- ec2Ssh(cancelCtx, instance, input)
		}(instance)
	}
	var errLast error
	var result []*ec2SshResult
	for range input.Instances {
		sshResult := <-resultChan
		if sshResult.Err != nil {
			Logger.Println("error:", sshResult.Err)
			errLast = sshResult.Err
		}
		result = append(result, sshResult)
	}
	//
	return result, errLast
}

type ec2SshResult struct {
	Err        error
	InstanceID string
	Stdout     []string
}

func ec2Ssh(ctx context.Context, instance *ec2.Instance, input *EC2SshInput) *ec2SshResult {
	result := &ec2SshResult{
		InstanceID: *instance.InstanceId,
	}
	sshCmd := []string{
		"ssh",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "StrictHostKeyChecking=no",
	}
	if input.Key != "" {
		sshCmd = append(sshCmd, []string{"-i", input.Key, "-o", "IdentitiesOnly=yes"}...)
	}
	target := input.User + "@"
	if input.PrivateIP {
		target += *instance.PrivateIpAddress
	} else {
		target += *instance.PublicDnsName
	}
	sshCmd = append(sshCmd, target)
	failureMessage := "failure"
	if len(input.Instances) == 1 {
		failureMessage = fmt.Sprintf("failure on %s", *instance.InstanceId)
	}
	sshCmd = append(sshCmd, ec2SshRemoteCmd(input.Cmd, input.Stdin, failureMessage))
	//
	cmd := exec.CommandContext(ctx, sshCmd[0], sshCmd[1:]...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		Logger.Println("error:", err)
		result.Err = err
		return result
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		Logger.Println("error:", err)
		result.Err = err
		return result
	}
	//
	resultLock := &sync.RWMutex{}
	done := make(chan error)
	tail := func(kind string, buf *bufio.Reader) {
		for {
			line, err := buf.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					done <- err
				} else {
					done <- nil
				}
				return
			}
			if strings.Contains(line, "Permission denied (publickey)") {
				done <- fmt.Errorf("ec2 Permission denied (publickey)")
				return
			}
			if len(input.Instances) > 1 {
				line = *instance.InstanceId + ": " + line
			}
			if !input.NoPrint && kind == "stdout" && input.AccumulateResult {
				resultLock.Lock()
				result.Stdout = append(result.Stdout, line)
				resultLock.Unlock()
			}
			input.PrintLock.Lock()
			switch kind {
			case "stderr":
				_, _ = fmt.Fprint(os.Stderr, line)
			case "stdout":
				_, _ = fmt.Fprint(os.Stdout, line)
			default:
				panic("unknown kind: " + kind)
			}
			input.PrintLock.Unlock()
		}
	}
	//
	go tail("stdout", bufio.NewReader(stdout))
	go tail("stderr", bufio.NewReader(stderr))
	err = cmd.Start()
	if err != nil {
		Logger.Println("error:", err)
		result.Err = err
		return result
	}
	for i := 0; i < 2; i++ {
		err := <-done
		if err != nil {
			Logger.Println("error:", err)
			result.Err = err
			return result
		}
	}
	err = cmd.Wait()
	if err != nil {
		Logger.Println("error:", err)
		result.Err = err
		return result
	}
	return result
}

func EC2SshLogin(instance *ec2.Instance, user string) error {
	if user == "" {
		user = EC2GetTag(instance.Tags, "user", "")
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

func EC2AmiUser(ctx context.Context, amiID string) (string, error) {
	out, err := EC2Client().DescribeImagesWithContext(ctx, &ec2.DescribeImagesInput{
		Filters: []*ec2.Filter{
			{Name: aws.String("image-id"), Values: []*string{aws.String(amiID)}},
		},
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	user := EC2GetTag(out.Images[0].Tags, "user", "")
	if user == "" {
		err := fmt.Errorf("no user for: %s", amiID)
		Logger.Println("error:", err)
		return "", err
	}
	return user, nil
}

func ec2AmiLambda(ctx context.Context, arch string) (string, error) {
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
		Owners: []*string{aws.String("137112412989")},
		Filters: []*ec2.Filter{
			{Name: aws.String("name"), Values: []*string{aws.String(ami)}},
			{Name: aws.String("architecture"), Values: []*string{aws.String(arch)}},
		},
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
	"focal":  "ubuntu/images/hvm-ssd/ubuntu-focal-20.04-",
	"bionic": "ubuntu/images/hvm-ssd/ubuntu-bionic-18.04-",
	"xenial": "ubuntu/images/hvm-ssd/ubuntu-xenial-16.04-",
	"trusty": "ubuntu/images/hvm-ssd/ubuntu-trusty-14.04-",
}

func keys(m map[string]string) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

func ec2AmiUbuntu(ctx context.Context, name, arch string) (string, error) {
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
			{Name: aws.String("architecture"), Values: []*string{aws.String(arch)}},
		},
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	sort.Slice(out.Images, func(i, j int) bool { return *out.Images[i].CreationDate > *out.Images[j].CreationDate })
	return *out.Images[0].ImageId, nil
}

func ec2AmiAmzn(ctx context.Context, arch string) (string, error) {
	out, err := EC2Client().DescribeImagesWithContext(ctx, &ec2.DescribeImagesInput{
		Owners: []*string{aws.String("137112412989")},
		Filters: []*ec2.Filter{
			{Name: aws.String("name"), Values: []*string{aws.String("amzn2-ami-hvm-2.0*-ebs")}},
			{Name: aws.String("architecture"), Values: []*string{aws.String(arch)}},
		},
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	sort.Slice(out.Images, func(i, j int) bool { return *out.Images[i].CreationDate > *out.Images[j].CreationDate })
	return *out.Images[0].ImageId, nil
}

func ec2AmiArch(ctx context.Context, arch string) (string, error) {
	if arch != EC2ArchAmd64 {
		err := fmt.Errorf("ec2 archlinux only supports amd64")
		Logger.Println("error:", err)
		return "", err
	}
	out, err := EC2Client().DescribeImagesWithContext(ctx, &ec2.DescribeImagesInput{
		Owners: []*string{aws.String("093273469852")},
		Filters: []*ec2.Filter{
			{Name: aws.String("name"), Values: []*string{aws.String("arch-linux-hvm-*-ebs")}},
			{Name: aws.String("architecture"), Values: []*string{aws.String(arch)}},
		},
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	sort.Slice(out.Images, func(i, j int) bool { return *out.Images[i].CreationDate > *out.Images[j].CreationDate })
	return *out.Images[0].ImageId, nil
}

func ec2AmiAlpine(ctx context.Context, arch string) (string, error) {
	out, err := EC2Client().DescribeImagesWithContext(ctx, &ec2.DescribeImagesInput{
		Owners: []*string{aws.String("538276064493")},
		Filters: []*ec2.Filter{
			{Name: aws.String("name"), Values: []*string{aws.String("alpine-ami-*")}},
			{Name: aws.String("architecture"), Values: []*string{aws.String(arch)}},
		},
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	var images []*ec2.Image
	for _, image := range out.Images {
		if strings.Contains(*image.Name, "edge") {
			images = append(images, image)
		}
	}
	sort.Slice(images, func(i, j int) bool { return *images[i].Name > *images[j].Name })
	return *images[0].ImageId, nil
}

func EC2AmiBase(ctx context.Context, name, arch string) (amiID string, sshUser string, err error) {
	switch arch {
	case EC2ArchAmd64:
	case EC2ArchArm64:
	default:
		err := fmt.Errorf("ec2 unknown cpu architecture: %s", arch)
		Logger.Println("error:", err)
		return "", "", err
	}
	switch name {
	case EC2AmiLambda:
		amiID, err = ec2AmiLambda(ctx, arch)
		sshUser = "user"
	case EC2AmiAmzn:
		amiID, err = ec2AmiAmzn(ctx, arch)
		sshUser = "user"
	case EC2AmiArch:
		amiID, err = ec2AmiArch(ctx, arch)
		sshUser = "arch"
	case EC2AmiAlpine:
		amiID, err = ec2AmiAlpine(ctx, arch)
		sshUser = "alpine"
	default:
		amiID, err = ec2AmiUbuntu(ctx, name, arch)
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
		Filters: []*ec2.Filter{{Name: aws.String("group-name"), Values: []*string{aws.String(name)}}},
	})
	if err != nil {
		return "", err
	}
	if len(out.SecurityGroups) != 1 {
		err = fmt.Errorf("didn't find exactly 1 security group for name: %s %s", name, Pformat(out.SecurityGroups))
		return "", err
	}
	return *out.SecurityGroups[0].GroupId, nil
}

func EC2Tags(tags []*ec2.Tag) string {
	var res []string
	for _, tag := range tags {
		if !Contains([]string{"Name", "aws:ec2spot:fleet-request-id", "creation-date"}, *tag.Key) {
			res = append(res, fmt.Sprintf("%s=%s", *tag.Key, *tag.Value))
		}
	}
	sort.Strings(res)
	return strings.Join(res, ",")
}

func EC2TagsAll(tags []*ec2.Tag) string {
	var res []string
	for _, tag := range tags {
		if *tag.Key != "Name" {
			res = append(res, fmt.Sprintf("%s=%s", *tag.Key, *tag.Value))
		}
	}
	return strings.Join(res, ",")
}

func EC2Name(tags []*ec2.Tag) string {
	for _, tag := range tags {
		if *tag.Key == "Name" {
			return *tag.Value
		}
	}
	return "-"
}

func EC2NameColored(instance *ec2.Instance) string {
	name := "-"
	for _, tag := range instance.Tags {
		if *tag.Key == "Name" {
			name = *tag.Value
			break
		}
	}
	switch *instance.State.Name {
	case "running":
		name = Green(name)
	case "pending":
		name = Cyan(name)
	default:
		name = Red(name)
	}
	return name
}

func EC2SecurityGroups(sgs []*ec2.GroupIdentifier) string {
	var res []string
	for _, sg := range sgs {
		if *sg.GroupName != "" {
			res = append(res, *sg.GroupName)
		} else {
			res = append(res, *sg.GroupId)
		}
	}
	if len(res) == 0 {
		return "-"
	}
	return strings.Join(res, ",")
}

func EC2Kind(instance *ec2.Instance) string {
	if instance.SpotInstanceRequestId != nil {
		return "spot"
	}
	return "ondemand"
}

type EC2WaitForSshInput struct {
	Selectors      []string
	MaxWaitSeconds int
	PrivateIP      bool
	User           string
	Key            string
	MaxConcurrency int
}

func EC2WaitSsh(ctx context.Context, input *EC2WaitForSshInput) ([]string, error) {
	start := time.Now()
	for {
		allInstances, err := EC2ListInstances(ctx, input.Selectors, "")
		if err != nil {
			return nil, err
		}
		//
		var instances []*ec2.Instance
		var pendingInstances []*ec2.Instance
		for _, instance := range allInstances {
			switch *instance.State.Name {
			case ec2.InstanceStateNameRunning:
				instances = append(instances, instance)
			case ec2.InstanceStateNamePending:
				pendingInstances = append(pendingInstances, instance)
			default:
			}
		}
		//
		var ips []string
		for _, instance := range instances {
			if input.PrivateIP {
				ips = append(ips, *instance.PrivateIpAddress)
			} else {
				ips = append(ips, *instance.PublicDnsName)
			}
		}
		// add an executable on PATH named `aws-ec2-ip-callback` which
		// will be invoked with the ipv4 of all instances to be waited
		// before each attempt
		_ = exec.Command("bash", "-c", fmt.Sprintf("aws-ec2-ip-callback %s && sleep 1", strings.Join(ips, " "))).Run()
		//
		results, err := EC2Ssh(context.Background(), &EC2SshInput{
			User:           input.User,
			TimeoutSeconds: 10,
			Instances:      instances,
			Cmd:            "whoami >/dev/null",
			PrivateIP:      input.PrivateIP,
			MaxConcurrency: input.MaxConcurrency,
			Key:            input.Key,
			PrintLock:      sync.RWMutex{},
			NoPrint:        true,
		})
		for _, result := range results {
			if result.Err == nil {
				fmt.Fprintf(os.Stderr, "ready: %s\n", Green(result.InstanceID))
			} else {
				fmt.Fprintf(os.Stderr, "unready: %s\n", Red(result.InstanceID))
			}
		}
		for _, instance := range pendingInstances {
			fmt.Fprintf(os.Stderr, "unready: %s\n", Red(*instance.InstanceId))
		}
		if err == nil && len(pendingInstances) == 0 {
			var ids []string
			for _, result := range results {
				ids = append(ids, result.InstanceID)
			}
			return ids, nil
		}
		if len(pendingInstances) == 0 && input.MaxWaitSeconds != 0 && time.Since(start) > time.Duration(input.MaxWaitSeconds)*time.Second {
			var ready []string
			var terminate []string
			for _, result := range results {
				if result.Err != nil {
					fmt.Fprintln(os.Stderr, "terminating unready instance:", result.InstanceID)
					terminate = append(terminate, result.InstanceID)
				} else {
					ready = append(ready, result.InstanceID)
				}
			}
			_, err = EC2Client().TerminateInstancesWithContext(ctx, &ec2.TerminateInstancesInput{
				InstanceIds: aws.StringSlice(terminate),
			})
			if err != nil {
				return nil, err
			}
			return ready, nil
		}
		time.Sleep(5 * time.Second)
	}
}

type EC2WaitForGoSshInput struct {
	Selectors      []string
	MaxWaitSeconds int
	User           string
	MaxConcurrency int
	RsaPrivKey     string
	Ed25519PrivKey string
}

func EC2WaitGoSsh(ctx context.Context, input *EC2WaitForGoSshInput) ([]string, error) {
	for {
		allInstances, err := EC2ListInstances(ctx, input.Selectors, "")
		if err != nil {
			return nil, err
		}
		//
		var instances []*ec2.Instance
		var pendingInstances []*ec2.Instance
		for _, instance := range allInstances {
			switch *instance.State.Name {
			case ec2.InstanceStateNameRunning:
				instances = append(instances, instance)
			case ec2.InstanceStateNamePending:
				pendingInstances = append(pendingInstances, instance)
			default:
			}
		}
		//
		var ips []string
		for _, instance := range instances {
			ips = append(ips, *instance.PublicDnsName)
		}
		// add an executable on PATH named `aws-ec2-ip-callback` which
		// will be invoked with the ipv4 of all instances to be waited
		// before each attempt
		_ = exec.Command("bash", "-c", fmt.Sprintf("aws-ec2-ip-callback %s && sleep 1", strings.Join(ips, " "))).Run()
		//
		err = EC2GoSsh(context.Background(), &EC2GoSshInput{
			User:           input.User,
			TimeoutSeconds: 10,
			Instances:      instances,
			Cmd:            "whoami >/dev/null",
			MaxConcurrency: input.MaxConcurrency,
			Stdout:         os.Stdout,
			Stderr:         os.Stderr,
			RsaPrivKey:     input.RsaPrivKey,
			Ed25519PrivKey: input.Ed25519PrivKey,
		})
		for _, instance := range pendingInstances {
			fmt.Fprintf(os.Stderr, "unready: %s\n", Red(*instance.InstanceId))
		}
		if err == nil && len(pendingInstances) == 0 {
			var ids []string
			for _, instance := range instances {
				ids = append(ids, *instance.InstanceId)
			}
			return ids, nil
		}
		time.Sleep(5 * time.Second)
	}
}

type EC2NewAmiInput struct {
	Selectors []string
	Wait      bool
}

func EC2NewAmi(ctx context.Context, input *EC2NewAmiInput) (string, error) {
	out, err := EC2ListInstances(ctx, input.Selectors, "stopped")
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	if len(out) != 1 {
		err := fmt.Errorf("not exactly 1 instance %s", Pformat(out))
		Logger.Println("error:", err)
		return "", err
	}
	i := out[0]
	name := EC2Name(i.Tags)
	image, err := EC2Client().CreateImageWithContext(ctx, &ec2.CreateImageInput{
		Name:        aws.String(fmt.Sprintf("%s__%d", name, time.Now().Unix())),
		Description: aws.String(name),
		InstanceId:  i.InstanceId,
		NoReboot:    aws.Bool(false),
		TagSpecifications: []*ec2.TagSpecification{{
			ResourceType: aws.String(ec2.ResourceTypeImage),
			Tags: []*ec2.Tag{{
				Key:   aws.String("user"),
				Value: aws.String(EC2GetTag(i.Tags, "user", "")),
			}},
		}},
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	if input.Wait {
		for {
			status, err := EC2Client().DescribeImagesWithContext(ctx, &ec2.DescribeImagesInput{
				ImageIds: []*string{image.ImageId},
			})
			if err != nil {
				Logger.Println("error:", err)
				return "", err
			}
			if *status.Images[0].State == ec2.ImageStateAvailable {
				break
			}
			Logger.Println("wait for image", time.Now())
			time.Sleep(1 * time.Second)
		}
	}
	return *image.ImageId, nil
}

func EC2ListSg(ctx context.Context) ([]*ec2.SecurityGroup, error) {
	var res []*ec2.SecurityGroup
	var token *string
	for {
		out, err := EC2Client().DescribeSecurityGroupsWithContext(ctx, &ec2.DescribeSecurityGroupsInput{
			NextToken: token,
		})
		if err != nil {
			return nil, err
		}
		res = append(res, out.SecurityGroups...)
		if out.NextToken == nil {
			break
		}
		token = out.NextToken
	}
	return res, nil
}

type EC2SgRule struct {
	Proto string
	Port  int
	Cidr  string
}

type EC2EnsureSgInput struct {
	VpcName string
	SgName  string
	Rules   []EC2SgRule
}

func EC2EnsureSg(ctx context.Context, input *EC2EnsureSgInput) error {
	vpcID, err := VpcEnsure(ctx, input.VpcName, 0)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	sgID, err := EC2SgID(ctx, input.SgName)
	if err != nil {
		out, err := EC2Client().CreateSecurityGroupWithContext(ctx, &ec2.CreateSecurityGroupInput{
			GroupName:   aws.String(input.SgName),
			Description: aws.String(input.SgName),
			VpcId:       aws.String(vpcID),
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		err = Retry(ctx, func() error {
			_, err := EC2Client().CreateTagsWithContext(ctx, &ec2.CreateTagsInput{
				Resources: []*string{out.GroupId},
				Tags: []*ec2.Tag{{
					Key:   aws.String("Name"),
					Value: aws.String(input.SgName),
				}},
			})
			return err
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		sgID = *out.GroupId
	}
	sgs, err := EC2Client().DescribeSecurityGroupsWithContext(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []*ec2.Filter{{Name: aws.String("group-id"), Values: []*string{aws.String(sgID)}}},
	})
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if len(sgs.SecurityGroups) != 1 {
		err := fmt.Errorf("expected exactly 1 sg: %s", Pformat(sgs))
		Logger.Println("error:", err)
		return err
	}
	sg := sgs.SecurityGroups[0]
	delete := make(map[EC2SgRule]bool)
	for _, r := range sg.IpPermissions {
		if len(r.UserIdGroupPairs) > 0 {
			continue
		}
		if *r.FromPort != *r.ToPort {
			err := fmt.Errorf("expected ports to match: %s", Pformat(r))
			Logger.Println("error:", err)
			return err
		}
		for _, ip := range r.IpRanges {
			delete[EC2SgRule{*r.IpProtocol, int(*r.ToPort), *ip.CidrIp}] = true
		}
		for _, ip := range r.Ipv6Ranges {
			delete[EC2SgRule{*r.IpProtocol, int(*r.ToPort), *ip.CidrIpv6}] = true
		}
		for _, ip := range r.UserIdGroupPairs {
			delete[EC2SgRule{*r.IpProtocol, int(*r.ToPort), *ip.GroupId}] = true
		}
		for _, ip := range r.PrefixListIds {
			delete[EC2SgRule{*r.IpProtocol, int(*r.ToPort), *ip.PrefixListId}] = true
		}
	}
	for _, r := range input.Rules {
		key := EC2SgRule{r.Proto, r.Port, r.Cidr}
		_, ok := delete[key]
		if ok {
			delete[key] = false
		} else {
			_, err := EC2Client().AuthorizeSecurityGroupIngressWithContext(ctx, &ec2.AuthorizeSecurityGroupIngressInput{
				CidrIp:     aws.String(r.Cidr),
				FromPort:   aws.Int64(int64(r.Port)),
				ToPort:     aws.Int64(int64(r.Port)),
				IpProtocol: aws.String(r.Proto),
				GroupId:    sg.GroupId,
			})
			if err != nil {
				aerr, ok := err.(awserr.Error)
				if !ok || aerr.Code() != "InvalidPermission.Duplicate" {
					Logger.Println("error:", err)
					return err
				}
			}
		}
	}
	for k, v := range delete {
		if !v {
			continue
		}
		_, err := EC2Client().RevokeSecurityGroupIngressWithContext(ctx, &ec2.RevokeSecurityGroupIngressInput{
			CidrIp:     aws.String(k.Cidr),
			FromPort:   aws.Int64(int64(k.Port)),
			ToPort:     aws.Int64(int64(k.Port)),
			IpProtocol: aws.String(k.Proto),
			GroupId:    sg.GroupId,
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	return nil
}

type EC2GoSshInput struct {
	NoTTY          bool
	Instances      []*ec2.Instance
	Cmd            string
	TimeoutSeconds int
	MaxConcurrency int
	User           string
	Stdout         io.WriteCloser
	Stderr         io.WriteCloser
	Stdin          string
	RsaPrivKey     string
	Ed25519PrivKey string
}

func pubKey(privKey string) (ssh.AuthMethod, error) {
	signer, err := ssh.ParsePrivateKey([]byte(privKey))
	if err != nil {
		return nil, err
	}
	return ssh.PublicKeys(signer), nil
}

func EC2GoSsh(ctx context.Context, input *EC2GoSshInput) error {
	if len(input.Instances) == 0 {
		err := fmt.Errorf("no instances")
		Logger.Println("error:", err)
		return err
	}
	if !strings.HasPrefix(input.Cmd, "#!") && !strings.HasPrefix(input.Cmd, "set ") {
		input.Cmd = "#!/bin/bash\nset -eou pipefail\n" + input.Cmd
	}
	if input.User == "" {
		input.User = EC2GetTag(input.Instances[0].Tags, "user", "")
		for _, instance := range input.Instances[1:] {
			user := EC2GetTag(instance.Tags, "user", "")
			if input.User != user {
				err := fmt.Errorf("not all instance users are the same, want: %s, got: %s", input.User, user)
				Logger.Println("error:", err)
				return err
			}
		}
		input.User = EC2GetTag(input.Instances[0].Tags, "user", "")
		if input.User == "" {
			err := fmt.Errorf("no user provied and no user tag available")
			Logger.Println("error:", err)
			return err
		}
	}
	//
	auth := []ssh.AuthMethod{}
	//
	if input.RsaPrivKey != "" {
		key, err := pubKey(input.RsaPrivKey)
		if err == nil {
			auth = append(auth, key)
		}
	} else if input.Ed25519PrivKey != "" {
		key, err := pubKey(input.Ed25519PrivKey)
		if err == nil {
			auth = append(auth, key)
		}
	} else {
		err := fmt.Errorf("one of RsaPrivKey or Ed25519PrivKey must be provided")
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
			done <- ec2GoSsh(cancelCtx, config, instance, input)
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

func ec2GoSsh(ctx context.Context, config *ssh.ClientConfig, instance *ec2.Instance, input *EC2GoSshInput) error {
	sshConn, err := sshDialContext(ctx, "tcp", fmt.Sprintf("%s:22", *instance.PublicDnsName), config)
	if err != nil {
		return err
	}
	defer func() { _ = sshConn.Close() }()
	//
	sshSession, err := sshConn.NewSession()
	if err != nil {
		return err
	}
	defer func() { _ = sshSession.Close() }()
	//
	if err != nil {
		return err
	}
	sshSession.Stdout = input.Stdout
	sshSession.Stderr = input.Stderr
	//
	if !input.NoTTY {
		err = sshSession.RequestPty("xterm", 80, 24, ssh.TerminalModes{})
		if err != nil {
			return err
		}
	}
	cmd := ec2SshRemoteCmd(input.Cmd, input.Stdin, *instance.InstanceId)
	runContext, runCancel := context.WithCancel(ctx)
	defer runCancel()
	go func() {
		<-runContext.Done()
		_ = sshSession.Close()
		_ = sshConn.Close()
	}()
	err = sshSession.Run(cmd)
	if err != nil {
		return err
	}
	return nil
}
