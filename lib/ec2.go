package lib

import (
	"bufio"
	"bytes"
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

	EC2AmiAlpineEdge = "alpine" // edge
	EC2AmiLambda     = "lambda"
	EC2AmiAmzn       = "amzn"
	EC2AmiArch       = "arch"

	EC2AmiUbuntuJammy  = "jammy"
	EC2AmiUbuntuFocal  = "focal"
	EC2AmiUbuntuBionic = "bionic"
	EC2AmiUbuntuXenial = "xenial"
	EC2AmiUbuntuTrusty = "trusty"

	EC2AmiDebianBullseye = "bullseye"
	EC2AmiDebianBuster   = "buster"
	EC2AmiDebianStretch  = "stretch"

	EC2AmiAlpine3160 = "alpine-3.16.0"
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
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2DescribeSpotFleet"}
		defer d.Log()
	}
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
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2DescribeSpotFleetActiveInstances"}
		defer d.Log()
	}
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
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2ListInstances"}
		defer d.Log()
	}
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
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2DescribeInstances"}
		defer d.Log()
	}
	Logger.Println("describe instances for", len(instanceIDs), "instanceIDs")
	if len(instanceIDs) >= 1000 {
		err := fmt.Errorf("cannot list 1000 instances by id")
		Logger.Println("error:", err)
		return nil, err
	}
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

func EC2WaitState(ctx context.Context, instanceIDs []string, state string) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2WaitState"}
		defer d.Log()
	}
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

func ec2FinalizeSpotFleet(ctx context.Context, spotFleetRequestId *string) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "ec2FinalizeSpotFleet"}
		defer d.Log()
	}
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
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2TeardownSpotFleet"}
		defer d.Log()
	}
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
	err = EC2WaitState(ctx, ids, ec2.InstanceStateNameRunning)
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
	if doDebug {
		d := &Debug{start: time.Now(), name: "ec2SpotFleetHistoryErrors"}
		defer d.Log()
	}
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

func ec2WaitSpotFleet(ctx context.Context, spotFleetRequestId *string, num int) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "ec2WaitSpotFleet"}
		defer d.Log()
	}
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
	if config.UserName == "alpine" || config.UserName == "admin" { // alpine and debian use /dev/xvda
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
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2RequestSpotFleet"}
		defer d.Log()
	}
	if strings.Contains(config.Name, "::") {
		err := fmt.Errorf("name cannot contain '::', got: %s", config.Name)
		Logger.Println("error:", err)
		return nil, err
	}
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
		RoleName: aws.String(EC2SpotFleetTaggingRole),
	})
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	launchSpecs := []*ec2.SpotFleetLaunchSpecification{}
	for _, subnetId := range config.SubnetIds {
		launchSpec := &ec2.SpotFleetLaunchSpecification{
			ImageId:             aws.String(config.AmiID),
			KeyName:             aws.String(config.Key),
			SubnetId:            aws.String(subnetId),
			InstanceType:        aws.String(config.InstanceType),
			UserData:            aws.String(makeInit(config)),
			EbsOptimized:        aws.Bool(true),
			SecurityGroups:      []*ec2.GroupIdentifier{{GroupId: aws.String(config.SgID)}},
			BlockDeviceMappings: makeBlockDeviceMapping(config),
			TagSpecifications: []*ec2.SpotFleetTagSpecification{{
				ResourceType: aws.String(ec2.ResourceTypeInstance),
				Tags:         makeTags(config),
			}},
		}
		if config.Profile != "" {
			launchSpec.IamInstanceProfile = &ec2.IamInstanceProfileSpecification{Name: aws.String(config.Profile)}
		}
		launchSpecs = append(launchSpecs, launchSpec)
	}
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
	Logger.Println("type:", config.InstanceType)
	Logger.Println("subnets:", config.SubnetIds)
	launchSpecs[0].UserData = nil
	launchSpecs[0].SubnetId = nil
	Logger.Println("requst spot fleet", Pformat(launchSpecs[0]))
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	err = ec2WaitSpotFleet(ctx, spotFleet.SpotFleetRequestId, config.NumInstances)
	if err != nil {
		Logger.Println("error:", err)
		err2 := EC2TeardownSpotFleet(context.Background(), spotFleet.SpotFleetRequestId)
		if err2 != nil {
			Logger.Println("error:", err2)
			return nil, err2
		}
		return nil, err
	}
	err = ec2FinalizeSpotFleet(ctx, spotFleet.SpotFleetRequestId)
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
	for _, instanceType := range []string{"i3", "i3en", "i4i", "c5d", "m5d", "r5d", "z1d", "c6gd", "c6id", "m6gd", "r6gd", "c5ad", "is4gen", "im4gn"} {
		if instanceType == strings.Split(config.InstanceType, ".")[0] {
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
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2NewInstances"}
		defer d.Log()
	}
	if strings.Contains(config.Name, "::") {
		err := fmt.Errorf("name cannot contain '::', got: %s", config.Name)
		Logger.Println("error:", err)
		return nil, err
	}
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
		SecurityGroupIds:    []*string{&config.SgID},
		BlockDeviceMappings: makeBlockDeviceMapping(config),
		MinCount:            aws.Int64(int64(config.NumInstances)),
		MaxCount:            aws.Int64(int64(config.NumInstances)),
		TagSpecifications: []*ec2.TagSpecification{{
			ResourceType: aws.String(ec2.ResourceTypeInstance),
			Tags:         makeTags(config),
		}},
	}
	if config.Profile != "" {
		runInstancesInput.IamInstanceProfile = &ec2.IamInstanceProfileSpecification{Name: aws.String(config.Profile)}
	}
	reservation, err := EC2Client().RunInstancesWithContext(ctx, runInstancesInput)
	runInstancesInput.UserData = nil
	Logger.Println("run instances", Pformat(runInstancesInput))
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
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2Rsync"}
		defer d.Log()
	}
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
	if input.MaxConcurrency == 0 {
		input.MaxConcurrency = 32
	}
	if input.TimeoutSeconds != 0 {
		timeoutCtx, timeoutCancel := context.WithTimeout(ctx, time.Duration(input.TimeoutSeconds)*time.Second)
		defer timeoutCancel()
		ctx = timeoutCtx
	}
	resultChan := make(chan *ec2SshResult, len(input.Instances))
	concurrency := semaphore.NewWeighted(int64(input.MaxConcurrency))
	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	for _, instance := range input.Instances {
		go func(instance *ec2.Instance) {
			defer func() {
				if r := recover(); r != nil {
					logRecover(r)
				}
			}()
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
			errLast = sshResult.Err
		}
		result = append(result, sshResult)
	}
	return result, errLast
}

func ec2Rsync(ctx context.Context, instance *ec2.Instance, input *EC2RsyncInput) *ec2SshResult {
	if doDebug {
		d := &Debug{start: time.Now(), name: "ec2Rsync"}
		defer d.Log()
	}
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
		rsyncCmd = append(rsyncCmd, splitWhiteSpace(os.Getenv("RSYNC_OPTIONS"))...)
	}
	target := input.User + "@"
	if input.PrivateIP {
		target += *instance.PrivateIpAddress
	} else {
		target += *instance.PublicDnsName
	}
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
	for _, src := range splitWhiteSpace(source) {
		src = strings.Trim(src, " ")
		if src != "" {
			rsyncCmd = append(rsyncCmd, src)
		}
	}
	for _, dst := range splitWhiteSpace(destination) {
		dst = strings.Trim(dst, " ")
		if dst != "" {
			rsyncCmd = append(rsyncCmd, dst)
		}
	}
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
	resultLock := &sync.RWMutex{}
	done := make(chan error)
	tail := func(kind string, buf *bufio.Reader) {
		// defer func() {}()
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
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2Scp"}
		defer d.Log()
	}
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
	if input.MaxConcurrency == 0 {
		input.MaxConcurrency = 32
	}
	if input.TimeoutSeconds != 0 {
		timeoutCtx, timeoutCancel := context.WithTimeout(ctx, time.Duration(input.TimeoutSeconds)*time.Second)
		defer timeoutCancel()
		ctx = timeoutCtx
	}
	resultChan := make(chan *ec2SshResult, len(input.Instances))
	concurrency := semaphore.NewWeighted(int64(input.MaxConcurrency))
	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	for _, instance := range input.Instances {
		go func(instance *ec2.Instance) {
			defer func() {
				if r := recover(); r != nil {
					logRecover(r)
				}
			}()
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
	return result, errLast
}

func ec2Scp(ctx context.Context, instance *ec2.Instance, input *EC2ScpInput) *ec2SshResult {
	if doDebug {
		d := &Debug{start: time.Now(), name: "ec2Scp"}
		defer d.Log()
	}
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
	resultLock := &sync.RWMutex{}
	done := make(chan error)
	tail := func(kind string, buf *bufio.Reader) {
		// defer func() {}()
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

const remoteCmdTemplateFailureMessage = `
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

const remoteCmdTemplate = `
mkdir -p /dev/shm/.cmds || echo $fail_msg
path=/dev/shm/.cmds/$(cat /proc/sys/kernel/random/uuid)
input=$path.input
echo %s | base64 -d > $path  || echo $fail_msg
echo %s | base64 -d > $input || echo $fail_msg
cat $input | bash $path
code=$?
if [ $code != 0 ]; then
    exit $code
fi
`

func ec2SshRemoteCmdFailureMessage(cmd, stdin, failureMessage string) string {
	return fmt.Sprintf(
		remoteCmdTemplateFailureMessage,
		failureMessage,
		base64.StdEncoding.EncodeToString([]byte(cmd)),
		base64.StdEncoding.EncodeToString([]byte(stdin)),
	)
}

func ec2SshRemoteCmd(cmd, stdin string) string {
	return fmt.Sprintf(
		remoteCmdTemplate,
		base64.StdEncoding.EncodeToString([]byte(cmd)),
		base64.StdEncoding.EncodeToString([]byte(stdin)),
	)
}

func EC2Ssh(ctx context.Context, input *EC2SshInput) ([]*ec2SshResult, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2Ssh"}
		defer d.Log()
	}
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
	if !strings.HasPrefix(input.Cmd, "#!") && !strings.HasPrefix(input.Cmd, "set ") {
		input.Cmd = "#!/bin/bash\nset -eou pipefail\n" + input.Cmd
	}
	if input.MaxConcurrency == 0 {
		input.MaxConcurrency = 32
	}
	if input.TimeoutSeconds != 0 {
		timeoutCtx, timeoutCancel := context.WithTimeout(ctx, time.Duration(input.TimeoutSeconds)*time.Second)
		defer timeoutCancel()
		ctx = timeoutCtx
	}
	resultChan := make(chan *ec2SshResult, len(input.Instances))
	concurrency := semaphore.NewWeighted(int64(input.MaxConcurrency))
	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	for _, instance := range input.Instances {
		go func(instance *ec2.Instance) {
			defer func() {
				if r := recover(); r != nil {
					logRecover(r)
				}
			}()
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
	return result, errLast
}

type ec2SshResult struct {
	Err        error
	InstanceID string
	Stdout     []string
}

func ec2Ssh(ctx context.Context, instance *ec2.Instance, input *EC2SshInput) *ec2SshResult {
	if doDebug {
		d := &Debug{start: time.Now(), name: "ec2Ssh"}
		defer d.Log()
	}
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
	sshCmd = append(sshCmd, ec2SshRemoteCmdFailureMessage(input.Cmd, input.Stdin, failureMessage))
	cmd := exec.CommandContext(ctx, sshCmd[0], sshCmd[1:]...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		Logger.Println("error:", err)
		result.Err = err
		return result
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		result.Err = err
		return result
	}
	resultLock := &sync.RWMutex{}
	done := make(chan error)
	tail := func(kind string, buf *bufio.Reader) {
		// defer func() {}()
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
	go tail("stdout", bufio.NewReader(stdout))
	go tail("stderr", bufio.NewReader(stderr))
	err = cmd.Start()
	if err != nil {
		result.Err = err
		return result
	}
	for i := 0; i < 2; i++ {
		err := <-done
		if err != nil {
			result.Err = err
			return result
		}
	}
	err = cmd.Wait()
	if err != nil {
		result.Err = err
		return result
	}
	return result
}

func EC2SshLogin(instance *ec2.Instance, user, key string) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2SshLogin"}
		defer d.Log()
	}
	if user == "" {
		user = EC2GetTag(instance.Tags, "user", "")
		if user == "" {
			err := fmt.Errorf("no user provied and no user tag available")
			Logger.Println("error:", err)
			return err
		}
	}
	sshCmd := []string{
		"ssh",
		user + "@" + *instance.PublicDnsName,
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "StrictHostKeyChecking=no",
	}
	if key != "" {
		sshCmd = append(sshCmd, []string{"-i", key, "-o", "IdentitiesOnly=yes"}...)
	}
	cmd := exec.Command(sshCmd[0], sshCmd[1:]...)
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
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2AmiUser"}
		defer d.Log()
	}
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
	if doDebug {
		d := &Debug{start: time.Now(), name: "ec2AmiLambda"}
		defer d.Log()
	}
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
	return *out.Images[0].ImageId, nil
}

var alpines = map[string]string{
	"alpine-3.16.0": "alpine-3.16.0-",
}

var ubuntus = map[string]string{
	"jammy":  "ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-",
	"focal":  "ubuntu/images/hvm-ssd/ubuntu-focal-20.04-",
	"bionic": "ubuntu/images/hvm-ssd/ubuntu-bionic-18.04-",
	"xenial": "ubuntu/images/hvm-ssd/ubuntu-xenial-16.04-",
	"trusty": "ubuntu/images/hvm-ssd/ubuntu-trusty-14.04-",
}

var debians = map[string]string{
	"bullseye": "debian-11-",
	"buster":   "debian-10-",
	"stretch":  "debian-9-",
}

func keys(m map[string]string) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

func ec2AmiUbuntu(ctx context.Context, name, arch string) (string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "ec2AmiUbuntu"}
		defer d.Log()
	}
	fragment, ok := ubuntus[name]
	if !ok {
		err := fmt.Errorf("bad ubuntu name %s, should be one of: %v", name, keys(ubuntus))
		Logger.Println("error:", err)
		return "", err
	}
	out, err := EC2Client().DescribeImagesWithContext(ctx, &ec2.DescribeImagesInput{
		Owners: []*string{aws.String("099720109477")},
		Filters: []*ec2.Filter{
			{Name: aws.String("name"), Values: []*string{aws.String(fmt.Sprintf("%s*", fragment))}},
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

func ec2AmiDebian(ctx context.Context, name, arch string) (string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "ec2AmiDebian"}
		defer d.Log()
	}
	fragment, ok := debians[name]
	if !ok {
		err := fmt.Errorf("bad debian name %s, should be one of: %v", name, keys(debians))
		Logger.Println("error:", err)
		return "", err
	}
	_ = fragment
	out, err := EC2Client().DescribeImagesWithContext(ctx, &ec2.DescribeImagesInput{
		Owners: []*string{aws.String("136693071363")},
		Filters: []*ec2.Filter{
			{Name: aws.String("name"), Values: []*string{aws.String(fmt.Sprintf("%s*", fragment))}},
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
	if doDebug {
		d := &Debug{start: time.Now(), name: "ec2AmiAmzn"}
		defer d.Log()
	}
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

// uplinklabs has stopped publishing these images, so make sure no
// ami-ID other than the following is ever used, as they are the last published.
// final release of: Release 2021.06.02 ebs hvm x86_64 stable
var ec2AmiArchFinalPublishedImages = []string{
	"ami-0aa7dbd2db78fa7fb",
	"ami-008ef391bf64d8b7f",
	"ami-0bc142e116312d7e6",
	"ami-023693ce180f3adb1",
	"ami-07b08bb244fc72525",
	"ami-0f0259bc20bcc0114",
	"ami-0209cf5105e3a5872",
	"ami-0a10dbf824c9cc3fc",
	"ami-0b46d60197650c7b2",
	"ami-0622746594aec927a",
	"ami-0668288fd748abc0a",
	"ami-0f472c7bb5ef62781",
	"ami-058d618623e52c423",
	"ami-00894b4c7df5dbb94",
	"ami-0fae3a42c0f7438ee",
	"ami-016f15543452da599",
	"ami-00cfc0bbf81a9d32c",
	"ami-0343ae980006cdd80",
	"ami-0745450a9dd2e9595",
	"ami-0abb8c3a6b6e5e6f3",
}

func ec2AmiArch(ctx context.Context, arch string) (string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "ec2AmiArch"}
		defer d.Log()
	}
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
	amiID := *out.Images[0].ImageId
	if !Contains(ec2AmiArchFinalPublishedImages, amiID) {
		return "", fmt.Errorf("ami-id %s was not in the list of the final published images from https://www.uplinklabs.net/projects/arch-linux-on-ec2/ %v", amiID, ec2AmiArchFinalPublishedImages)
	}
	return amiID, nil
}

func ec2AmiAlpineEdge(ctx context.Context, arch string) (string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "ec2AmiAlpineEdge"}
		defer d.Log()
	}
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
	sort.Slice(images, func(i, j int) bool { return *images[i].CreationDate > *images[j].CreationDate })
	return *images[0].ImageId, nil
}

func ec2AmiAlpine(ctx context.Context, name, arch string) (string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "ec2AmiAlpine"}
		defer d.Log()
	}
	out, err := EC2Client().DescribeImagesWithContext(ctx, &ec2.DescribeImagesInput{
		Owners: []*string{aws.String("538276064493")},
		Filters: []*ec2.Filter{
			{Name: aws.String("name"), Values: []*string{aws.String(alpines[name] + "*")}},
			{Name: aws.String("architecture"), Values: []*string{aws.String(arch)}},
		},
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	var images []*ec2.Image
	for _, image := range out.Images {
		if strings.Contains(*image.Name, "-tiny-") {
			images = append(images, image)
		}
	}
	sort.Slice(images, func(i, j int) bool { return *images[i].CreationDate > *images[j].CreationDate })
	return *images[0].ImageId, nil
}

func EC2AmiBase(ctx context.Context, name, arch string) (amiID string, sshUser string, err error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2AmiBase"}
		defer d.Log()
	}
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
	case EC2AmiAlpineEdge:
		amiID, err = ec2AmiAlpineEdge(ctx, arch)
		sshUser = "alpine"
	default:
		_, okAlpine := alpines[name]
		_, okUbuntu := ubuntus[name]
		_, okDebian := debians[name]
		switch {
		case okAlpine:
			amiID, err = ec2AmiAlpine(ctx, name, arch)
			sshUser = "alpine"
		case okUbuntu:
			amiID, err = ec2AmiUbuntu(ctx, name, arch)
			sshUser = "ubuntu"
		case okDebian:
			amiID, err = ec2AmiDebian(ctx, name, arch)
			sshUser = "admin"
		default:
			err := fmt.Errorf("unknown ami name, should be one of: \"ami-ID | arch | amzn | lambda | deeplearning | bionic | xenial | trusty | focal | jammy | bullseye | buster | stretch | alpine-3.16.0\", got: %s", name)
			Logger.Println("error:", err)
			return "", "", err
		}
	}
	return amiID, sshUser, err
}

func EC2ZonesWithInstance(ctx context.Context, instanceType string) (zones []string, err error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2ZonesWithInstance"}
		defer d.Log()
	}
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
	if len(zones) == 0 {
		err := fmt.Errorf("no zones with instance type: %s", instanceType)
		Logger.Println("error:", err)
		return nil, err
	}
	return zones, nil
}

func EC2SgID(ctx context.Context, vpcName, sgName string) (string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2SgID"}
		defer d.Log()
	}
	if strings.HasPrefix(sgName, "sg-") {
		return sgName, nil
	}
	vpcID, err := VpcID(ctx, vpcName)
	if err != nil {
		return "", err
	}
	out, err := EC2Client().DescribeSecurityGroupsWithContext(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []*ec2.Filter{
			{Name: aws.String("group-name"), Values: []*string{aws.String(sgName)}},
			{Name: aws.String("vpc-id"), Values: []*string{aws.String(vpcID)}},
		},
	})
	if err != nil {
		return "", err
	}
	if len(out.SecurityGroups) != 1 {
		err = fmt.Errorf("%s security group for name: %s %s", ErrPrefixDidntFindExactlyOne, sgName, Pformat(out.SecurityGroups))
		return "", err
	}
	return *out.SecurityGroups[0].GroupId, nil
}

func EC2Tags(tags []*ec2.Tag) string {
	var res []string
	for _, tag := range tags {
		if !Contains([]string{"Name", "aws:ec2spot:fleet-request-id", "creation-date", "user"}, *tag.Key) {
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

type EC2WaitSshInput struct {
	Selectors      []string
	MaxWaitSeconds int
	PrivateIP      bool
	User           string
	Key            string
	MaxConcurrency int
}

func EC2WaitSsh(ctx context.Context, input *EC2WaitSshInput) ([]string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2WaitSsh"}
		defer d.Log()
	}
	start := time.Now()
	for {
		now := time.Now()
		allInstances, err := EC2ListInstances(ctx, input.Selectors, "")
		if err != nil {
			return nil, err
		}
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
		results, err := EC2Ssh(context.Background(), &EC2SshInput{
			User:           input.User,
			TimeoutSeconds: 5,
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
			fmt.Fprintf(os.Stderr, "pending: %s\n", Cyan(*instance.InstanceId))
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
			if len(ready) == 0 {
				err := fmt.Errorf("no instances became ready")
				Logger.Println("error:", err)
				return nil, err
			}
			return ready, nil
		}
		secondsToWait := 5 - (time.Since(now).Seconds())
		if secondsToWait > 0 {
			time.Sleep(time.Duration(secondsToWait) * time.Second)
		}
	}
}

type EC2WaitGoSshInput struct {
	Selectors      []string
	MaxWaitSeconds int
	User           string
	MaxConcurrency int
	RsaPrivKey     string
	Ed25519PrivKey string
	Stdout         io.Writer
	Stderr         io.Writer
}

func EC2WaitGoSsh(ctx context.Context, input *EC2WaitGoSshInput) ([]string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2WaitGoSsh"}
		defer d.Log()
	}
	start := time.Now()
	if input.Stderr == nil {
		input.Stderr = os.Stderr
	}
	if input.Stdout == nil {
		input.Stdout = os.Stdout
	}
	for {
		now := time.Now()
		allInstances, err := EC2ListInstances(ctx, input.Selectors, "")
		if err != nil {
			return nil, err
		}
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
		var targetAddrs []string
		for _, instance := range instances {
			targetAddrs = append(targetAddrs, *instance.PublicDnsName)
		}
		// add an executable on PATH named `aws-ec2-ip-callback` which
		// will be invoked with the ipv4 of all instances to be waited
		// before each attempt
		_ = exec.Command("bash", "-c", fmt.Sprintf("aws-ec2-ip-callback %s && sleep 1", strings.Join(targetAddrs, " "))).Run()
		results, err := EC2GoSsh(context.Background(), &EC2GoSshInput{
			NoTTY:          true,
			User:           input.User,
			TimeoutSeconds: 5,
			TargetAddrs:    targetAddrs,
			Cmd:            "whoami >/dev/null",
			MaxConcurrency: input.MaxConcurrency,
			Stdout:         input.Stdout,
			Stderr:         input.Stderr,
			RsaPrivKey:     input.RsaPrivKey,
			Ed25519PrivKey: input.Ed25519PrivKey,
		})
		for _, result := range results {
			if result.Err == nil {
				Logger.Printf("ready: %s\n", Green(result.TargetAddr))
			} else {
				Logger.Printf("unready: %s\n", Red(result.TargetAddr))
			}
		}
		for _, instance := range pendingInstances {
			Logger.Printf("pending: %s\n", Cyan(*instance.InstanceId))
		}
		if err == nil && len(pendingInstances) == 0 {
			return targetAddrs, nil
		}
		if len(pendingInstances) == 0 && input.MaxWaitSeconds != 0 && time.Since(start) > time.Duration(input.MaxWaitSeconds)*time.Second {
			var ready []string
			var terminate []string
			for _, result := range results {
				if result.Err != nil {
					Logger.Printf("terminating unready instance:", result.TargetAddr)
					terminate = append(terminate, result.TargetAddr)
				} else {
					ready = append(ready, result.TargetAddr)
				}
			}
			instances, err := EC2ListInstances(context.Background(), terminate, "running")
			if err != nil {
				Logger.Println("error:", err)
				return nil, err
			}
			var ids []*string
			for _, instance := range instances {
				ids = append(ids, instance.InstanceId)
			}
			if len(ids) > 0 {
				_, err = EC2Client().TerminateInstancesWithContext(context.Background(), &ec2.TerminateInstancesInput{
					InstanceIds: ids,
				})
				if err != nil {
					Logger.Println("error:", err)
					return nil, err
				}
			}
			if len(ready) == 0 {
				err := fmt.Errorf("no instances became ready")
				Logger.Println("error:", err)
				return nil, err
			}
			return ready, nil
		}
		secondsToWait := 5 - (time.Since(now).Seconds())
		if secondsToWait > 0 {
			time.Sleep(time.Duration(secondsToWait) * time.Second)
		}
	}
}

type EC2NewAmiInput struct {
	Selectors []string
	Wait      bool
}

func EC2NewAmi(ctx context.Context, input *EC2NewAmiInput) (string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2NewAmi"}
		defer d.Log()
	}
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
		start := time.Now()
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
			Logger.Println("wait for image", fmt.Sprintf("t+%d", int(time.Since(start).Seconds())))
			time.Sleep(1 * time.Second)
		}
	}
	return *image.ImageId, nil
}

func EC2ListSgs(ctx context.Context) ([]*ec2.SecurityGroup, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2ListSgs"}
		defer d.Log()
	}
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
	Proto  string `json:"proto"`
	Port   int    `json:"port"`
	Source string `json:"source"`
}

func EC2SgRules(p *ec2.IpPermission) ([]EC2SgRule, error) {
	var rules []EC2SgRule
	if !(p.FromPort == nil && p.ToPort == nil) && *p.FromPort != *p.ToPort {
		err := fmt.Errorf("expected ports to match: %s", Pformat(p))
		Logger.Println("error:", err)
		return nil, err
	}
	toPort := 0
	if p.ToPort != nil {
		toPort = int(*p.ToPort)
	}
	proto := ""
	if *p.IpProtocol != "-1" {
		proto = *p.IpProtocol
	}
	for _, ip := range p.IpRanges {
		rules = append(rules, EC2SgRule{proto, toPort, *ip.CidrIp})
	}
	for _, ip := range p.Ipv6Ranges {
		rules = append(rules, EC2SgRule{proto, toPort, *ip.CidrIpv6})
	}
	for _, ip := range p.UserIdGroupPairs {
		rules = append(rules, EC2SgRule{proto, toPort, *ip.GroupId})
	}
	for _, ip := range p.PrefixListIds {
		rules = append(rules, EC2SgRule{proto, toPort, *ip.PrefixListId})
	}
	return rules, nil
}

func (r EC2SgRule) String() string {
	proto := ""
	if r.Proto != "-1" {
		proto = r.Proto
	}
	port := ""
	if r.Port != 0 {
		port = fmt.Sprint(r.Port)
	}
	source := r.Source
	return fmt.Sprintf("%s:%s:%s", proto, port, source)
}

type ec2EnsureSgInput struct {
	InfraSetName string
	VpcName      string
	SgName       string
	Rules        []EC2SgRule
}

func EC2EnsureSgInput(infraSetName, vpcName, sgName string, rules []string) (*ec2EnsureSgInput, error) {
	input := &ec2EnsureSgInput{
		InfraSetName: infraSetName,
		VpcName:      vpcName,
		SgName:       sgName,
	}
	for _, r := range rules {
		proto, port, source, err := SplitTwice(r, ":")
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		rule := EC2SgRule{
			Proto:  proto,
			Source: source,
		}
		if port != "" {
			rule.Port = Atoi(port)
		}
		input.Rules = append(input.Rules, rule)
	}
	return input, nil
}

func EC2EnsureSg(ctx context.Context, input *ec2EnsureSgInput, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2EnsureSg"}
		defer d.Log()
	}
	for _, rule := range input.Rules {
		if rule.Port == 0 && rule.Proto != "" {
			err := fmt.Errorf("you must specify both port and proto or neither, got: %#v", rule)
			Logger.Println("error:", err)
			return err
		}
	}
	vpcID, err := VpcID(ctx, input.VpcName)
	if err != nil {
		if !strings.HasPrefix(err.Error(), ErrPrefixDidntFindExactlyOne) {
			Logger.Println("error:", err)
			return err
		}
	}
	tags := []*ec2.TagSpecification{{
		ResourceType: aws.String(ec2.ResourceTypeSecurityGroup),
		Tags: []*ec2.Tag{
			{
				Key:   aws.String("Name"),
				Value: aws.String(input.SgName),
			},
			{
				Key:   aws.String(infraSetTagName),
				Value: aws.String(input.InfraSetName),
			},
		},
	}}
	sgID, err := EC2SgID(ctx, input.VpcName, input.SgName)
	if err != nil {
		if !strings.HasPrefix(err.Error(), ErrPrefixDidntFindExactlyOne) {
			return err
		}
		if !preview {
			out, err := EC2Client().CreateSecurityGroupWithContext(ctx, &ec2.CreateSecurityGroupInput{
				GroupName:         aws.String(input.SgName),
				Description:       aws.String(input.SgName),
				VpcId:             aws.String(vpcID),
				TagSpecifications: tags,
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			sgID = *out.GroupId
			// wait for security group instantiation
			err = RetryAttempts(ctx, 11, func() error {
				sgs, err := EC2Client().DescribeSecurityGroupsWithContext(ctx, &ec2.DescribeSecurityGroupsInput{
					Filters: []*ec2.Filter{
						{Name: aws.String("group-id"), Values: []*string{aws.String(sgID)}},
						{Name: aws.String("vpc-id"), Values: []*string{aws.String(vpcID)}},
					},
				})
				if err != nil {
					return err
				}
				if len(sgs.SecurityGroups) == 0 {
					return fmt.Errorf("wait for security group instantiation found for: %#v", input)
				}
				return nil
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"created security group:", input.VpcName, input.SgName)
	}
	sgs, err := EC2Client().DescribeSecurityGroupsWithContext(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []*ec2.Filter{
			{Name: aws.String("group-id"), Values: []*string{aws.String(sgID)}},
			{Name: aws.String("vpc-id"), Values: []*string{aws.String(vpcID)}},
		},
	})
	if err != nil {
		_, ok := err.(awserr.Error)
		if !ok || !preview || strings.Split(err.Error(), "\n")[0] != "InvalidParameterValue: vpc-id" {
			Logger.Println("error:", err)
			return err
		}
	}
	if !preview && len(sgs.SecurityGroups) != 1 {
		err := fmt.Errorf("expected exactly 1 sg: %s", Pformat(sgs))
		Logger.Println("error:", err)
		return err
	}
	if preview && len(sgs.SecurityGroups) > 1 {
		err := fmt.Errorf("expected exactly 1 sg: %s", Pformat(sgs))
		Logger.Println("error:", err)
		return err
	}
	delete := make(map[EC2SgRule]bool)
	if len(sgs.SecurityGroups) == 1 {
		sg := sgs.SecurityGroups[0]
		for _, r := range sg.IpPermissions {
			rules, err := EC2SgRules(r)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			for _, rule := range rules {
				delete[rule] = true
			}
		}
	}
	for _, r := range input.Rules {
		_, ok := delete[r]
		if ok {
			delete[r] = false
		} else {
			if !preview {
				sg := sgs.SecurityGroups[0]
				var port *int64
				if r.Port != 0 {
					port = aws.Int64(int64(r.Port))
				}
				proto := "-1" // all ports
				if r.Proto != "" {
					proto = r.Proto
				}
				var ipPermission *ec2.IpPermission
				if strings.HasPrefix(r.Source, "sg-") {
					ipPermission = &ec2.IpPermission{
						UserIdGroupPairs: []*ec2.UserIdGroupPair{{GroupId: aws.String(r.Source)}},
						FromPort:         port,
						ToPort:           port,
						IpProtocol:       aws.String(proto),
					}
				} else {
					ipPermission = &ec2.IpPermission{
						IpRanges:   []*ec2.IpRange{{CidrIp: aws.String(r.Source)}},
						FromPort:   port,
						ToPort:     port,
						IpProtocol: aws.String(proto),
					}
				}
				_, err := EC2Client().AuthorizeSecurityGroupIngressWithContext(ctx, &ec2.AuthorizeSecurityGroupIngressInput{
					GroupId:       sg.GroupId,
					IpPermissions: []*ec2.IpPermission{ipPermission},
				})
				if err != nil {
					aerr, ok := err.(awserr.Error)
					if !ok || aerr.Code() != "InvalidPermission.Duplicate" {
						Logger.Println("error:", err)
						return err
					}
				}
				// wait for rule instantiation
				err = RetryAttempts(ctx, 11, func() error {
					sgs, err := EC2Client().DescribeSecurityGroupsWithContext(ctx, &ec2.DescribeSecurityGroupsInput{
						Filters: []*ec2.Filter{
							{Name: aws.String("group-id"), Values: []*string{aws.String(sgID)}},
							{Name: aws.String("vpc-id"), Values: []*string{aws.String(vpcID)}},
						},
					})
					if err != nil {
						return err
					}
					switch len(sgs.SecurityGroups) {
					case 0:
						return fmt.Errorf("didn't find 1 security groups for: %#v", input)
					case 1:
						sg := sgs.SecurityGroups[0]
						for _, perms := range sg.IpPermissions {
							rules, err := EC2SgRules(perms)
							if err != nil {
								Logger.Println("error:", err)
								return err
							}
							for _, rule := range rules {
								if rule == r {
									return nil
								}
							}
						}
						return fmt.Errorf("didn't find rule: %#v", r)
					default:
						return fmt.Errorf("didn't find 0 or 1 security groups for: %#v", input)
					}
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
			}
			Logger.Println(PreviewString(preview)+"authorize ingress:", r)
		}
	}
	for k, v := range delete {
		if !v {
			continue
		}
		if !preview {
			var port *int64
			if k.Port != 0 {
				port = aws.Int64(int64(k.Port))
			}
			proto := "-1" // all ports
			if k.Proto != "" {
				proto = k.Proto
			}
			sg := sgs.SecurityGroups[0]
			var permission *ec2.IpPermission
			if strings.HasPrefix(k.Source, "sg-") {
				permission = &ec2.IpPermission{
					UserIdGroupPairs: []*ec2.UserIdGroupPair{{GroupId: aws.String(k.Source)}},
					FromPort:         port,
					ToPort:           port,
					IpProtocol:       aws.String(proto),
				}
			} else {
				permission = &ec2.IpPermission{
					IpRanges:   []*ec2.IpRange{{CidrIp: aws.String(k.Source)}},
					FromPort:   port,
					ToPort:     port,
					IpProtocol: aws.String(proto),
				}
			}
			_, err := EC2Client().RevokeSecurityGroupIngressWithContext(ctx, &ec2.RevokeSecurityGroupIngressInput{
				GroupId:       sg.GroupId,
				IpPermissions: []*ec2.IpPermission{permission},
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			// wait for rule de-instantiation
			err = RetryAttempts(ctx, 11, func() error {
				sgs, err := EC2Client().DescribeSecurityGroupsWithContext(ctx, &ec2.DescribeSecurityGroupsInput{
					Filters: []*ec2.Filter{
						{Name: aws.String("group-id"), Values: []*string{aws.String(sgID)}},
						{Name: aws.String("vpc-id"), Values: []*string{aws.String(vpcID)}},
					},
				})
				if err != nil {
					return err
				}
				switch len(sgs.SecurityGroups) {
				case 0:
					return fmt.Errorf("didn't find 1 security groups for: %#v", input)
				case 1:
					sg := sgs.SecurityGroups[0]
					for _, perms := range sg.IpPermissions {
						rules, err := EC2SgRules(perms)
						if err != nil {
							Logger.Println("error:", err)
							return err
						}
						for _, rule := range rules {
							if rule == k {
								return fmt.Errorf("still found rule %#v", rule)
							}
						}
					}
					return nil
				default:
					return fmt.Errorf("didn't find 0 or 1 security groups for: %#v", input)
				}
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"deauthorize ingress:", k)
	}
	return nil
}

func EC2DeleteSg(ctx context.Context, vpcName, sgName string, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2DeleteSg"}
		defer d.Log()
	}
	sgID, err := EC2SgID(ctx, vpcName, sgName)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if !preview {
		_, err := EC2Client().DeleteSecurityGroupWithContext(ctx, &ec2.DeleteSecurityGroupInput{
			GroupId: aws.String(sgID),
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	Logger.Println(PreviewString(preview)+"deleted security-group:", sgName, vpcName)
	return nil
}

type EC2GoSshInput struct {
	NoTTY          bool
	TargetAddrs    []string
	Cmd            string
	TimeoutSeconds int
	MaxConcurrency int
	User           string
	Stdout         io.Writer
	Stderr         io.Writer
	Stdin          string
	RsaPrivKey     string
	Ed25519PrivKey string
}

type ec2GoSshResult struct {
	Err        error
	TargetAddr string
}

func pubKey(privKey string) (ssh.AuthMethod, error) {
	signer, err := ssh.ParsePrivateKey([]byte(privKey))
	if err != nil {
		return nil, err
	}
	return ssh.PublicKeys(signer), nil
}

func EC2GoSsh(ctx context.Context, input *EC2GoSshInput) ([]*ec2GoSshResult, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2GoSsh"}
		defer d.Log()
	}
	if input.Stderr == nil {
		input.Stderr = os.Stderr
	}
	if input.Stdout == nil {
		input.Stdout = os.Stdout
	}
	if len(input.TargetAddrs) == 0 {
		return nil, fmt.Errorf("no instances")
	}
	if !strings.HasPrefix(input.Cmd, "#!") && !strings.HasPrefix(input.Cmd, "set ") {
		input.Cmd = "#!/bin/bash\nset -eou pipefail\n" + input.Cmd
	}
	if input.User == "" {
		return nil, fmt.Errorf("expected user")
	}
	auth := []ssh.AuthMethod{}
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
		return nil, err
	}
	config := &ssh.ClientConfig{
		User:            input.User,
		Auth:            auth,
		Timeout:         5 * time.Second,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	if input.MaxConcurrency == 0 {
		input.MaxConcurrency = 32
	}
	if input.TimeoutSeconds != 0 {
		timeoutCtx, timeoutCancel := context.WithTimeout(ctx, time.Duration(input.TimeoutSeconds)*time.Second)
		defer timeoutCancel()
		ctx = timeoutCtx
	}
	// done := make(chan error, len(input.Instances))
	resultChan := make(chan *ec2GoSshResult, len(input.TargetAddrs))
	concurrency := semaphore.NewWeighted(int64(input.MaxConcurrency))
	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	for _, addr := range input.TargetAddrs {
		addr := addr
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logRecover(r)
				}
			}()
			err := concurrency.Acquire(cancelCtx, 1)
			if err != nil {
				resultChan <- &ec2GoSshResult{Err: err, TargetAddr: addr}
				return
			}
			defer concurrency.Release(1)
			err = ec2GoSsh(cancelCtx, config, addr, input)
			resultChan <- &ec2GoSshResult{Err: err, TargetAddr: addr}
		}()
	}
	var errLast error
	var result []*ec2GoSshResult
	for range input.TargetAddrs {
		sshResult := <-resultChan
		if sshResult.Err != nil {
			errLast = sshResult.Err
		}
		result = append(result, sshResult)
	}
	return result, errLast
}

func sshDialContext(ctx context.Context, network, addr string, config *ssh.ClientConfig) (*ssh.Client, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "sshDialContext"}
		defer d.Log()
	}
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

func ec2GoSsh(ctx context.Context, config *ssh.ClientConfig, targetAddr string, input *EC2GoSshInput) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "ec2GoSsh"}
		defer d.Log()
	}
	sshConn, err := sshDialContext(ctx, "tcp", fmt.Sprintf("%s:22", targetAddr), config)
	if err != nil {
		return err
	}
	defer func() { _ = sshConn.Close() }()
	sshSession, err := sshConn.NewSession()
	if err != nil {
		return err
	}
	defer func() { _ = sshSession.Close() }()
	if err != nil {
		return err
	}
	sshSession.Stdout = input.Stdout
	sshSession.Stderr = input.Stderr
	if !input.NoTTY {
		err = sshSession.RequestPty("xterm", 80, 24, ssh.TerminalModes{})
		if err != nil {
			return err
		}
	}
	cmd := ec2SshRemoteCmd(input.Cmd, input.Stdin)
	runContext, runCancel := context.WithCancel(ctx)
	defer runCancel()
	go func() {
		// defer func() {}()
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

func EC2DeleteKeypair(ctx context.Context, keypairName string, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2DeleteKeypair"}
		defer d.Log()
	}
	out, err := EC2Client().DescribeKeyPairsWithContext(ctx, &ec2.DescribeKeyPairsInput{
		KeyNames: []*string{aws.String(keypairName)},
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != "InvalidKeyPair.NotFound" {
			Logger.Println("error:", err)
			return err
		}
	}
	if len(out.KeyPairs) > 0 {
		if !preview {
			_, err := EC2Client().DeleteKeyPairWithContext(ctx, &ec2.DeleteKeyPairInput{
				KeyName: aws.String(keypairName),
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"deleted keypair:", keypairName)
	}
	return nil
}

func EC2EnsureKeypair(ctx context.Context, infraSetName, keyName, pubkeyContent string, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2EnsureKeypair"}
		defer d.Log()
	}
	pubkey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pubkeyContent))
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	out, err := EC2Client().DescribeKeyPairsWithContext(ctx, &ec2.DescribeKeyPairsInput{
		KeyNames: []*string{aws.String(keyName)},
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != "InvalidKeyPair.NotFound" {
			Logger.Println("error:", err)
			return err
		}
	}
	tags := []*ec2.TagSpecification{{
		ResourceType: aws.String(ec2.ResourceTypeKeyPair),
		Tags: []*ec2.Tag{{
			Key:   aws.String(infraSetTagName),
			Value: aws.String(infraSetName),
		}},
	}}
	switch len(out.KeyPairs) {
	case 0:
		if !preview {
			_, err := EC2Client().ImportKeyPairWithContext(ctx, &ec2.ImportKeyPairInput{
				KeyName:           aws.String(keyName),
				PublicKeyMaterial: []byte(pubkeyContent),
				TagSpecifications: tags,
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"created keypair:", keyName)
		return nil
	case 1:
		switch pubkey.Type() {
		case ssh.KeyAlgoED25519:
			remoteFingerprint := *out.KeyPairs[0].KeyFingerprint
			if remoteFingerprint[len(remoteFingerprint)-1] == '=' {
				remoteFingerprint = remoteFingerprint[:len(remoteFingerprint)-1]
			}
			localFingerprint := strings.SplitN(ssh.FingerprintSHA256(pubkey), ":", 2)[1]
			if remoteFingerprint != localFingerprint {
				if !preview {
					_, err := EC2Client().DeleteKeyPairWithContext(ctx, &ec2.DeleteKeyPairInput{
						KeyName: aws.String(keyName),
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
					_, err = EC2Client().ImportKeyPairWithContext(ctx, &ec2.ImportKeyPairInput{
						KeyName:           aws.String(keyName),
						PublicKeyMaterial: []byte(pubkeyContent),
						TagSpecifications: tags,
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
					Logger.Println(PreviewString(preview)+"updated keypair:", keyName, remoteFingerprint, "=>", localFingerprint)
				}
			}
			return nil
		case ssh.KeyAlgoRSA:
			f, err := os.CreateTemp("/tmp", "libaws_")
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			defer func() { _ = os.Remove(f.Name()) }()
			_, err = f.WriteString(pubkeyContent)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			cmd := exec.Command("bash", "-c", fmt.Sprintf(`ssh-keygen -e -f %s -m pkcs8 | openssl pkey -pubin -outform der | openssl md5 -c | cut -d" " -f2`, f.Name()))
			var stdout bytes.Buffer
			cmd.Stdout = &stdout
			err = cmd.Run()
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			remoteFingerprint := *out.KeyPairs[0].KeyFingerprint
			localFingerprint := strings.TrimRight(stdout.String(), "\n")
			if remoteFingerprint != localFingerprint {
				if !preview {
					_, err := EC2Client().DeleteKeyPairWithContext(ctx, &ec2.DeleteKeyPairInput{
						KeyName: aws.String(keyName),
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
					_, err = EC2Client().ImportKeyPairWithContext(ctx, &ec2.ImportKeyPairInput{
						KeyName:           aws.String(keyName),
						PublicKeyMaterial: []byte(pubkeyContent),
						TagSpecifications: tags,
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
					Logger.Println(PreviewString(preview)+"updated keypair:", keyName, remoteFingerprint, "=>", localFingerprint)
				}
			}
			return nil
		default:
			err := fmt.Errorf("bad key type: %s", pubkey.Type())
			Logger.Println("error:", err)
			return err
		}
	default:
		err := fmt.Errorf("more than 1 key found for: %s %s", keyName, Pformat(out))
		Logger.Println("error:", err)
		return err
	}
}
