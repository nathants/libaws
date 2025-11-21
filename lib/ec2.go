package lib

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/sync/semaphore"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/gofrs/uuid"
)

const (
	EC2ArchAmd64 = "x86_64"
	EC2ArchArm64 = "arm64"

	EC2AmiAmzn2023 = "amzn2023"
	EC2AmiAmzn2    = "amzn2"

	EC2AmiUbuntuJammy  = "jammy"
	EC2AmiUbuntuFocal  = "focal"
	EC2AmiUbuntuBionic = "bionic"
	EC2AmiUbuntuXenial = "xenial"
	EC2AmiUbuntuTrusty = "trusty"

	EC2AmiDebianTrixie   = "trixie"
	EC2AmiDebianBookworm = "bookworm"
	EC2AmiDebianBullseye = "bullseye"
	EC2AmiDebianBuster   = "buster"
	EC2AmiDebianStretch  = "stretch"
)

var ec2RegexpAlpine = regexp.MustCompile(`alpine\-\d\d?\.\d\d?\.\d\d?`)

var ec2Client *ec2.Client
var ec2ClientLock sync.Mutex

func EC2ClientExplicit(accessKeyID, accessKeySecret, region string) *ec2.Client {
	return ec2.NewFromConfig(*SessionExplicit(accessKeyID, accessKeySecret, region))
}

func EC2Client() *ec2.Client {
	ec2ClientLock.Lock()
	defer ec2ClientLock.Unlock()
	if ec2Client == nil {
		ec2Client = ec2.NewFromConfig(*Session())
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
	TempKey        bool
	InstanceType   ec2types.InstanceType
	SubnetIds      []string
	Gigs           int
	Throughput     int
	Iops           int
	Init           string
	Tags           []EC2Tag
	Profile        string
	SecondsTimeout int
	Spot           bool
}

func EC2DescribeSpotFleet(ctx context.Context, spotFleetRequestId *string) (*ec2types.SpotFleetRequestConfig, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2DescribeSpotFleet"}
		d.Start()
		defer d.End()
	}
	Logger.Println("describe spot fleet", *spotFleetRequestId)
	var output *ec2.DescribeSpotFleetRequestsOutput
	err := Retry(ctx, func() error {
		var err error
		output, err = EC2Client().DescribeSpotFleetRequests(ctx, &ec2.DescribeSpotFleetRequestsInput{
			SpotFleetRequestIds: []string{*spotFleetRequestId},
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
	return &output.SpotFleetRequestConfigs[0], nil
}

func EC2DescribeSpotFleetActiveInstances(ctx context.Context, spotFleetRequestId *string) ([]ec2types.ActiveInstance, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2DescribeSpotFleetActiveInstances"}
		d.Start()
		defer d.End()
	}
	Logger.Println("describe spot fleet instances", *spotFleetRequestId)
	var instances []ec2types.ActiveInstance
	var nextToken *string
	for {
		var output *ec2.DescribeSpotFleetInstancesOutput
		err := Retry(ctx, func() error {
			var err error
			output, err = EC2Client().DescribeSpotFleetInstances(ctx, &ec2.DescribeSpotFleetInstancesInput{
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

func EC2ListInstances(ctx context.Context, selectors []string, state ec2types.InstanceStateName) ([]ec2types.Instance, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2ListInstances"}
		d.Start()
		defer d.End()
	}
	var filterss [][]ec2types.Filter
	if len(selectors) == 0 {
		if state != "" {
			filterss = append(filterss, []ec2types.Filter{
				{Name: aws.String("instance-state-name"), Values: []string{string(state)}},
			})
		} else {
			filterss = append(filterss, []ec2types.Filter{{}})
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
		if kind == KindTags {
			for i, selector := range selectors {
				if !strings.Contains(selector, "=") {
					selectors[i] = fmt.Sprintf("Name=%s", selector)
				}
			}
		}
		for chunk := range slices.Chunk(selectors, 195) { // max aws api params per query is 200
			var filterKind ec2types.Filter
			var filters []ec2types.Filter
			if state != "" {
				filters = append(filters, ec2types.Filter{Name: aws.String("instance-state-name"), Values: []string{string(state)}})
			}
			for _, selector := range chunk {
				if kind == KindTags {
					parts := strings.Split(selector, "=")
					k := parts[0]
					v := parts[1]
					filters = append(filters, ec2types.Filter{
						Name:   aws.String(fmt.Sprintf("tag:%s", k)),
						Values: []string{v},
					})
				} else {
					filterKind.Name = aws.String(string(kind))
					filterKind.Values = append(filterKind.Values, selector)
				}
			}
			filters = append(filters, filterKind)
			filterss = append(filterss, filters)
		}
	}
	var instances []ec2types.Instance
	for _, filters := range filterss {
		var nextToken *string
		for {
			var output *ec2.DescribeInstancesOutput
			err := Retry(ctx, func() error {
				var err error
				output, err = EC2Client().DescribeInstances(ctx, &ec2.DescribeInstancesInput{
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
	sort.SliceStable(instances, func(i, j int) bool {
		return *instances[i].InstanceId < *instances[j].InstanceId
	})
	sort.SliceStable(instances, func(i, j int) bool {
		return EC2GetTag(instances[i].Tags, "Name", "") < EC2GetTag(instances[j].Tags, "Name", "")
	})
	sort.SliceStable(instances, func(i, j int) bool {
		return instances[i].LaunchTime.UnixNano() > instances[j].LaunchTime.UnixNano()
	})
	return instances, nil
}

func ec2Tags(tags []ec2types.Tag) map[string]string {
	val := map[string]string{}
	for _, tag := range tags {
		val[*tag.Key] = *tag.Value
	}
	return val
}

func EC2GetTag(tags []ec2types.Tag, key string, defaultValue string) string {
	val, ok := ec2Tags(tags)[key]
	if !ok {
		val = defaultValue
	}
	return val
}

func EC2DescribeInstances(ctx context.Context, instanceIDs []string) ([]ec2types.Instance, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2DescribeInstances"}
		d.Start()
		defer d.End()
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
		output, err = EC2Client().DescribeInstances(ctx, &ec2.DescribeInstancesInput{
			InstanceIds: instanceIDs,
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
	var instances []ec2types.Instance
	for _, reservation := range output.Reservations {
		instances = append(instances, reservation.Instances...)
	}
	return instances, nil
}

var ec2FailedStates = []ec2types.BatchState{
	ec2types.BatchStateCancelled,
	ec2types.BatchStateFailed,
	ec2types.BatchStateCancelledRunning,
	ec2types.BatchStateCancelledTerminatingInstances,
}

func EC2WaitState(ctx context.Context, instanceIDs []string, state ec2types.InstanceStateName) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2WaitState"}
		d.Start()
		defer d.End()
	}
	Logger.Println("wait for state", state, "for", len(instanceIDs), "instanceIDs")
	for range 300 {
		instances, err := EC2DescribeInstances(ctx, instanceIDs)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		ready := 0
		for _, instance := range instances {
			if instance.State.Name == state {
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
		d.Start()
		defer d.End()
	}
	Logger.Println("teardown spot fleet", *spotFleetRequestId)
	_, err := EC2Client().CancelSpotFleetRequests(ctx, &ec2.CancelSpotFleetRequestsInput{
		SpotFleetRequestIds: []string{*spotFleetRequestId},
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
		d.Start()
		defer d.End()
	}
	Logger.Println("teardown spot fleet", *spotFleetRequestId)
	_, err := EC2Client().CancelSpotFleetRequests(ctx, &ec2.CancelSpotFleetRequestsInput{
		SpotFleetRequestIds: []string{*spotFleetRequestId},
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
	err = EC2WaitState(ctx, ids, ec2types.InstanceStateNameRunning)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	_, err = EC2Client().TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: ids,
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
		d.Start()
		defer d.End()
	}

	t := time.Now().UTC().Add(-24 * time.Hour)
	timestamp := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	output, err := EC2Client().DescribeSpotFleetRequestHistory(ctx, &ec2.DescribeSpotFleetRequestHistoryInput{
		SpotFleetRequestId: spotFleetRequestId,
		StartTime:          aws.Time(timestamp),
	})
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	var errorsFound []string
	for _, record := range output.HistoryRecords {
		if record.EventInformation.EventDescription != nil {
			errorsFound = append(errorsFound, *record.EventInformation.EventDescription)
		}
	}
	if len(errorsFound) != 0 {
		err = fmt.Errorf("%s", strings.Join(errorsFound, "\n"))
		Logger.Println("error: spot fleet history error:", err)
		return err
	}
	return nil
}

func ec2WaitSpotFleet(ctx context.Context, spotFleetRequestId *string, num int) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "ec2WaitSpotFleet"}
		d.Start()
		defer d.End()
	}
	Logger.Println("wait for spot fleet", *spotFleetRequestId, "with", num, "instances")
	for range 300 {
		config, err := EC2DescribeSpotFleet(ctx, spotFleetRequestId)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		for _, state := range ec2FailedStates {
			if state == config.SpotFleetRequestState {
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

const tempkeyInit = `
  while true; do
    if ! grep 'PUBKEY' ~/.ssh/authorized_keys &>/dev/null; then
      echo -n 'PUBKEY' >> ~/.ssh/authorized_keys
    fi
    sleep 1
  done &>/dev/null </dev/null &
`

const timeoutInit = `
echo '# timeout will call this script before it $(sudo poweroff)s, and wait 60 seconds for this script to complete' | sudo tee -a /etc/timeout.sh >/dev/null
echo '#!/bin/bash
    warning="seconds remaining until timeout poweroff. [sudo journalctl -u timeout.service -f] to follow. increase /etc/timeout.seconds to delay. [date +%s > /tmp/seconds && sudo mv -f /tmp/seconds /etc/timeout.start.seconds] to reset, or [sudo systemctl {{stop,disable}} timeout.service] to cancel."
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
    echo sudo poweroff
    sleep 5
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

func makeBlockDeviceMapping(config *EC2Config) []ec2types.BlockDeviceMapping {
	deviceName := "/dev/sda1"
	if config.UserName == "alpine" || config.UserName == "admin" { // alpine and debian use /dev/xvda
		deviceName = "/dev/xvda"
	}
	return []ec2types.BlockDeviceMapping{{
		DeviceName: aws.String(deviceName),
		Ebs: &ec2types.EbsBlockDevice{
			DeleteOnTermination: aws.Bool(true),
			Encrypted:           aws.Bool(true),
			VolumeType:          ec2types.VolumeTypeGp3,
			Iops:                aws.Int32(int32(max(3000, int64(config.Iops)))),
			Throughput:          aws.Int32(int32(max(125, int64(config.Throughput)))),
			VolumeSize:          aws.Int32(int32(config.Gigs)),
		},
	}}
}

func EC2RequestSpotFleet(ctx context.Context, spotStrategy ec2types.AllocationStrategy, config *EC2Config) ([]ec2types.Instance, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2RequestSpotFleet"}
		d.Start()
		defer d.End()
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

	var allocStrategy ec2types.AllocationStrategy
	if !slices.Contains(allocStrategy.Values(), spotStrategy) {
		return nil, fmt.Errorf("invalid spot allocation strategy: %s", spotStrategy)
	}

	role, err := IamClient().GetRole(ctx, &iam.GetRoleInput{
		RoleName: aws.String(EC2SpotFleetTaggingRole),
	})
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	init, err := makeInit(config)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	launchSpecs := []ec2types.SpotFleetLaunchSpecification{}
	tags := makeTags(config)
	for _, subnetId := range config.SubnetIds {
		launchSpec := ec2types.SpotFleetLaunchSpecification{
			ImageId:      aws.String(config.AmiID),
			KeyName:      aws.String(config.Key),
			SubnetId:     aws.String(subnetId),
			InstanceType: config.InstanceType,
			UserData:     aws.String(init),
			EbsOptimized: aws.Bool(true),
			SecurityGroups: []ec2types.GroupIdentifier{{
				GroupId: aws.String(config.SgID),
			}},
			BlockDeviceMappings: makeBlockDeviceMapping(config),
			TagSpecifications: []ec2types.SpotFleetTagSpecification{
				{
					ResourceType: ec2types.ResourceTypeInstance,
					Tags:         tags,
				},
			},
		}
		if config.Profile != "" {
			launchSpec.IamInstanceProfile = &ec2types.IamInstanceProfileSpecification{Name: aws.String(config.Profile)}
		}
		launchSpecs = append(launchSpecs, launchSpec)
	}
	spotFleet, err := EC2Client().RequestSpotFleet(ctx, &ec2.RequestSpotFleetInput{
		SpotFleetRequestConfig: &ec2types.SpotFleetRequestConfigData{
			IamFleetRole:                     role.Role.Arn,
			LaunchSpecifications:             launchSpecs,
			AllocationStrategy:               spotStrategy,
			InstanceInterruptionBehavior:     ec2types.InstanceInterruptionBehaviorTerminate,
			Type:                             ec2types.FleetTypeRequest,
			TargetCapacity:                   aws.Int32(int32(config.NumInstances)),
			ReplaceUnhealthyInstances:        aws.Bool(false),
			TerminateInstancesWithExpiration: aws.Bool(false),
		},
	})
	Logger.Println("type:", config.InstanceType)
	Logger.Println("subnets:", config.SubnetIds)
	launchSpecs[0].UserData = nil
	launchSpecs[0].SubnetId = nil
	Logger.Println("request spot fleet", Pformat(launchSpecs[0]))
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

func makeInit(config *EC2Config) (string, error) {
	if config.UserName == "" {
		err := fmt.Errorf("makeInit needs a username")
		Logger.Println("error:", err)
		return "", err
	}
	init := config.Init
	for _, instanceType := range []string{"i3", "i3en", "i4i", "c5d", "m5d", "r5d", "z1d", "c6gd", "c6id", "m6gd", "r6gd", "c5ad", "is4gen", "im4gn"} {
		if instanceType == strings.Split(string(config.InstanceType), ".")[0] {
			init = nvmeInit + init
			break
		}
	}
	if config.SecondsTimeout != 0 {
		init = strings.Replace(timeoutInit, "TIMEOUT_SECONDS", fmt.Sprint(config.SecondsTimeout), 1) + init
	}
	if config.TempKey {
		pubKey, privKey, err := SshKeygenEd25519()
		if err != nil {
			Logger.Println("error:", err)
			return "", err
		}
		uid := uuid.Must(uuid.NewV4()).String()
		path := fmt.Sprintf("/tmp/libaws/%s", uid)
		err = os.MkdirAll(path, os.ModePerm)
		if err != nil {
			Logger.Println("error:", err)
			return "", err
		}
		config.Tags = append(config.Tags, EC2Tag{
			Name:  "ssh-id",
			Value: uid,
		})
		err = os.WriteFile(path+"/id_ed25519", []byte(privKey), 0600)
		if err != nil {
			Logger.Println("error:", err)
			return "", err
		}
		init = strings.ReplaceAll(tempkeyInit, "PUBKEY", pubKey) + init
	}
	init = base64.StdEncoding.EncodeToString([]byte(init))
	init = fmt.Sprintf("#!/bin/sh\nset -x; path=/tmp/$(cat /proc/sys/kernel/random/uuid); if which apk >/dev/null; then apk update && apk add curl git procps ncurses-terminfo coreutils sed grep less vim sudo bash && echo -e '%s ALL=(ALL) NOPASSWD:ALL\nroot ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers; fi; echo %s | base64 -d > $path; cd /home/%s; sudo -u %s bash -e $path 2>&1", config.UserName, init, config.UserName, config.UserName)
	init = base64.StdEncoding.EncodeToString([]byte(init))
	return init, nil
}

func makeTags(config *EC2Config) []ec2types.Tag {
	tags := []ec2types.Tag{
		{Key: aws.String("Name"), Value: aws.String(config.Name)},
		{Key: aws.String("user"), Value: aws.String(config.UserName)},
		{Key: aws.String("creation-date"), Value: aws.String(time.Now().UTC().Format(time.RFC3339))},
	}
	for _, tag := range config.Tags {
		tags = append(tags, ec2types.Tag{
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

func EC2NewInstances(ctx context.Context, config *EC2Config) ([]ec2types.Instance, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2NewInstances"}
		d.Start()
		defer d.End()
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
	init, err := makeInit(config)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	tags := makeTags(config)
	runInstancesInput := &ec2.RunInstancesInput{
		ImageId:             aws.String(config.AmiID),
		KeyName:             aws.String(config.Key),
		SubnetId:            aws.String(config.SubnetIds[0]),
		InstanceType:        config.InstanceType,
		UserData:            aws.String(init),
		EbsOptimized:        aws.Bool(true),
		SecurityGroupIds:    []string{config.SgID},
		BlockDeviceMappings: makeBlockDeviceMapping(config),
		MinCount:            aws.Int32(int32(config.NumInstances)),
		MaxCount:            aws.Int32(int32(config.NumInstances)),
		TagSpecifications: []ec2types.TagSpecification{
			{
				ResourceType: ec2types.ResourceTypeInstance,
				Tags:         tags,
			},
			{
				ResourceType: ec2types.ResourceTypeVolume,
				Tags:         tags,
			},
			{
				ResourceType: ec2types.ResourceTypeNetworkInterface,
				Tags:         tags,
			},
		},
	}
	if config.Profile != "" {
		runInstancesInput.IamInstanceProfile = &ec2types.IamInstanceProfileSpecification{Name: aws.String(config.Profile)}
	}
	if config.Spot {
		runInstancesInput.InstanceMarketOptions = &ec2types.InstanceMarketOptionsRequest{
			MarketType: ec2types.MarketTypeSpot,
			SpotOptions: &ec2types.SpotMarketOptions{
				SpotInstanceType:             ec2types.SpotInstanceTypeOneTime,
				InstanceInterruptionBehavior: ec2types.InstanceInterruptionBehaviorTerminate,
			},
		}
	}
	reservation, err := EC2Client().RunInstances(ctx, runInstancesInput)
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
	Instances        []ec2types.Instance
	TimeoutSeconds   int
	MaxConcurrency   int
	User             string
	PrivateIP        bool
	Key              string
	PrintLock        sync.Mutex
	AccumulateResult bool
}

func EC2Rsync(ctx context.Context, input *EC2RsyncInput) ([]*ec2SshResult, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2Rsync"}
		d.Start()
		defer d.End()
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
		go func() {
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
		}()
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

func ec2Rsync(ctx context.Context, instance ec2types.Instance, input *EC2RsyncInput) *ec2SshResult {
	if doDebug {
		d := &Debug{start: time.Now(), name: "ec2Rsync"}
		d.Start()
		defer d.End()
	}
	result := &ec2SshResult{
		InstanceID: *instance.InstanceId,
	}
	rsyncCmd := []string{
		"rsync",
		"-avh",
		"--delete",
	}
	tempKey := ec2EphemeralKey(instance.Tags)
	if tempKey != "" {
		rsyncCmd = append(rsyncCmd, []string{"-e", fmt.Sprintf("ssh -i %s -o IdentitiesOnly=yes -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no", tempKey)}...)
	} else if input.Key != "" {
		rsyncCmd = append(rsyncCmd, []string{"-e", fmt.Sprintf("ssh -i %s -o IdentitiesOnly=yes -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no", input.Key)}...)
	} else {
		rsyncCmd = append(rsyncCmd, []string{"-e", "ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no"}...)
	}
	if os.Getenv("RSYNC_OPTIONS") != "" {
		rsyncCmd = append(rsyncCmd, SplitWhiteSpace(os.Getenv("RSYNC_OPTIONS"))...)
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
	for _, src := range SplitWhiteSpace(source) {
		src = strings.Trim(src, " ")
		if src != "" {
			rsyncCmd = append(rsyncCmd, src)
		}
	}
	for _, dst := range SplitWhiteSpace(destination) {
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
	resultLock := &sync.Mutex{}
	done := make(chan error)
	tail := func(kind string, buf *bufio.Reader) {
		// defer func() {}()
		for {
			line, err := buf.ReadString('\n')
			if line != "" && !strings.HasSuffix(line, "\n") {
				line += "\n"
			}
			if strings.Contains(line, "Permission denied (publickey)") {
				done <- fmt.Errorf("ec2 Permission denied (publickey)")
				return
			} else if !strings.Contains(line, " to the list of known hosts.") {
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
			if err != nil {
				if err != io.EOF {
					done <- err
				} else {
					done <- nil
				}
				return
			}
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
	for range 2 {
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
	Instances        []ec2types.Instance
	TimeoutSeconds   int
	MaxConcurrency   int
	User             string
	PrivateIP        bool
	Key              string
	PrintLock        sync.Mutex
	AccumulateResult bool
}

func EC2Scp(ctx context.Context, input *EC2ScpInput) ([]*ec2SshResult, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2Scp"}
		d.Start()
		defer d.End()
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
		go func() {
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
		}()
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

func ec2Scp(ctx context.Context, instance ec2types.Instance, input *EC2ScpInput) *ec2SshResult {
	if doDebug {
		d := &Debug{start: time.Now(), name: "ec2Scp"}
		d.Start()
		defer d.End()
	}
	result := &ec2SshResult{
		InstanceID: *instance.InstanceId,
	}
	scpCmd := []string{
		"scp",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "StrictHostKeyChecking=no",
	}
	tempKey := ec2EphemeralKey(instance.Tags)
	if tempKey != "" {
		scpCmd = append(scpCmd, []string{"-o", "IdentitiesOnly=yes", "-i", tempKey}...)
	} else if input.Key != "" {
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
	resultLock := &sync.Mutex{}
	done := make(chan error)
	tail := func(kind string, buf *bufio.Reader) {
		// defer func() {}()
		for {
			line, err := buf.ReadString('\n')
			if line != "" && !strings.HasSuffix(line, "\n") {
				line += "\n"
			}
			if strings.Contains(line, "Permission denied (publickey)") {
				done <- fmt.Errorf("ec2 Permission denied (publickey)")
				return
			} else if !strings.Contains(line, " to the list of known hosts.") {
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
			if err != nil {
				if err != io.EOF {
					done <- err
				} else {
					done <- nil
				}
				return
			}
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
	for range 2 {
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
	Instances        []ec2types.Instance
	Cmd              string
	TimeoutSeconds   int
	MaxConcurrency   int
	User             string
	Stdin            string
	PrivateIP        bool
	Key              string
	AccumulateResult bool
	PrintLock        sync.Mutex
	NoPrint          bool
	IPNotID          bool
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
		d.Start()
		defer d.End()
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
		go func() {
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
		}()
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

func ec2EphemeralKey(tags []ec2types.Tag) string {
	for _, tag := range tags {
		if *tag.Key == "ssh-id" {
			path := fmt.Sprintf("/tmp/libaws/%s/id_ed25519", *tag.Value)
			if Exists(path) {
				return path
			}
		}
	}
	return ""
}

func ec2Ssh(ctx context.Context, instance ec2types.Instance, input *EC2SshInput) *ec2SshResult {
	if doDebug {
		d := &Debug{start: time.Now(), name: "ec2Ssh"}
		d.Start()
		defer d.End()
	}
	result := &ec2SshResult{
		InstanceID: *instance.InstanceId,
	}
	sshCmd := []string{
		"ssh",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "StrictHostKeyChecking=no",
	}
	tempKey := ec2EphemeralKey(instance.Tags)
	if tempKey != "" {
		sshCmd = append(sshCmd, []string{"-i", tempKey, "-o", "IdentitiesOnly=yes"}...)
	} else if input.Key != "" {
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
	resultLock := &sync.Mutex{}
	done := make(chan error)
	tail := func(kind string, buf *bufio.Reader) {
		// defer func() {}()
		for {
			line, err := buf.ReadString('\n')
			if line != "" && !strings.HasSuffix(line, "\n") {
				line += "\n"
			}
			if strings.Contains(line, "Permission denied (publickey)") {
				done <- fmt.Errorf("ec2 Permission denied (publickey)")
				return
			} else if !strings.Contains(line, " to the list of known hosts.") {
				if len(input.Instances) > 1 {
					if input.IPNotID {
						line = *instance.PublicIpAddress + ": " + line
					} else {
						line = *instance.InstanceId + ": " + line
					}
				}
				if kind == "stdout" && input.AccumulateResult {
					resultLock.Lock()
					result.Stdout = append(result.Stdout, line)
					resultLock.Unlock()
				}
				if !input.NoPrint {
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
			if err != nil {
				if err != io.EOF {
					done <- err
				} else {
					done <- nil
				}
				return
			}
		}
	}
	go tail("stdout", bufio.NewReader(stdout))
	go tail("stderr", bufio.NewReader(stderr))
	err = cmd.Start()
	if err != nil {
		result.Err = err
		return result
	}
	for range 2 {
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

func EC2SshLogin(instance ec2types.Instance, user, key string) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2SshLogin"}
		d.Start()
		defer d.End()
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
	tempKey := ec2EphemeralKey(instance.Tags)
	if tempKey != "" {
		sshCmd = append(sshCmd, []string{"-i", tempKey, "-o", "IdentitiesOnly=yes"}...)
	} else if key != "" {
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
		d.Start()
		defer d.End()
	}
	out, err := EC2Client().DescribeImages(ctx, &ec2.DescribeImagesInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("image-id"), Values: []string{amiID}},
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

func ec2AmiAmzn2023(ctx context.Context, arch string) (string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "ec2AmiAmzn2023"}
		d.Start()
		defer d.End()
	}
	out, err := EC2Client().DescribeImages(ctx, &ec2.DescribeImagesInput{
		Owners: []string{"137112412989"},
		Filters: []ec2types.Filter{
			{Name: aws.String("name"), Values: []string{"al2023-ami-2023*"}},
			{Name: aws.String("architecture"), Values: []string{arch}},
		},
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	if len(out.Images) == 0 {
		Logger.Println("error:", err)
		return "", err
	}
	sort.Slice(out.Images, func(i, j int) bool {
		return *out.Images[i].CreationDate > *out.Images[j].CreationDate
	})
	return *out.Images[0].ImageId, nil
}

var ubuntus = map[string]string{
	"jammy":  "ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-",
	"focal":  "ubuntu/images/hvm-ssd/ubuntu-focal-20.04-",
	"bionic": "ubuntu/images/hvm-ssd/ubuntu-bionic-18.04-",
	"xenial": "ubuntu/images/hvm-ssd/ubuntu-xenial-16.04-",
	"trusty": "ubuntu/images/hvm-ssd/ubuntu-trusty-14.04-",
}

var debians = map[string]string{
	"trixie":   "debian-13-",
	"bookworm": "debian-12-",
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
		d.Start()
		defer d.End()
	}
	fragment, ok := ubuntus[name]
	if !ok {
		err := fmt.Errorf("bad ubuntu name %s, should be one of: %v", name, keys(ubuntus))
		Logger.Println("error:", err)
		return "", err
	}
	out, err := EC2Client().DescribeImages(ctx, &ec2.DescribeImagesInput{
		Owners: []string{"099720109477"},
		Filters: []ec2types.Filter{
			{Name: aws.String("name"), Values: []string{fmt.Sprintf("%s*", fragment)}},
			{Name: aws.String("architecture"), Values: []string{arch}},
		},
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	sort.Slice(out.Images, func(i, j int) bool {
		return *out.Images[i].CreationDate > *out.Images[j].CreationDate
	})
	return *out.Images[0].ImageId, nil
}

func ec2AmiDebian(ctx context.Context, name, arch string) (string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "ec2AmiDebian"}
		d.Start()
		defer d.End()
	}
	fragment, ok := debians[name]
	if !ok {
		err := fmt.Errorf("bad debian name %s, should be one of: %v", name, keys(debians))
		Logger.Println("error:", err)
		return "", err
	}
	_ = fragment
	out, err := EC2Client().DescribeImages(ctx, &ec2.DescribeImagesInput{
		Owners: []string{"136693071363"},
		Filters: []ec2types.Filter{
			{Name: aws.String("name"), Values: []string{fmt.Sprintf("%s*", fragment)}},
			{Name: aws.String("architecture"), Values: []string{arch}},
		},
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	sort.Slice(out.Images, func(i, j int) bool {
		return *out.Images[i].CreationDate > *out.Images[j].CreationDate
	})
	return *out.Images[0].ImageId, nil
}

func ec2AmiAmzn2(ctx context.Context, arch string) (string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "ec2AmiAmzn"}
		d.Start()
		defer d.End()
	}
	out, err := EC2Client().DescribeImages(ctx, &ec2.DescribeImagesInput{
		Owners: []string{"137112412989"},
		Filters: []ec2types.Filter{
			{Name: aws.String("name"), Values: []string{"amzn2-ami-hvm-2.0*-ebs"}},
			{Name: aws.String("architecture"), Values: []string{arch}},
		},
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	sort.Slice(out.Images, func(i, j int) bool {
		return *out.Images[i].CreationDate > *out.Images[j].CreationDate
	})
	return *out.Images[0].ImageId, nil
}

func ec2AmiAlpine(ctx context.Context, name, arch string) (string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "ec2AmiAlpine"}
		d.Start()
		defer d.End()
	}
	out, err := EC2Client().DescribeImages(ctx, &ec2.DescribeImagesInput{
		Owners: []string{"538276064493"},
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("name"),
				Values: []string{name + "-*"},
			},
			{Name: aws.String("architecture"), Values: []string{arch}},
		},
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	var images []ec2types.Image
	for _, image := range out.Images {
		if strings.Contains(*image.Name, "-tiny-") && ec2RegexpAlpine.FindString(*image.Name) != "" {
			images = append(images, image)
		}
	}
	sort.SliceStable(images, func(i, j int) bool {
		return *images[i].CreationDate > *images[j].CreationDate
	})
	sort.SliceStable(images, func(i, j int) bool {
		versionsA := strings.SplitN(strings.SplitN(*images[i].Name, "-", 3)[1], ".", 3)
		versionsB := strings.SplitN(strings.SplitN(*images[j].Name, "-", 3)[1], ".", 3)
		versionA := fmt.Sprintf("%02d.%02d.%02d", Atoi(versionsA[0]), Atoi(versionsA[1]), Atoi(versionsA[2]))
		versionB := fmt.Sprintf("%02d.%02d.%02d", Atoi(versionsB[0]), Atoi(versionsB[1]), Atoi(versionsB[2]))
		return versionA > versionB
	})
	return *images[0].ImageId, nil
}

func EC2AmiBase(ctx context.Context, name, arch string) (amiID string, sshUser string, err error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2AmiBase"}
		d.Start()
		defer d.End()
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
	case EC2AmiAmzn2023:
		amiID, err = ec2AmiAmzn2023(ctx, arch)
		sshUser = "user"
	case EC2AmiAmzn2:
		amiID, err = ec2AmiAmzn2(ctx, arch)
		sshUser = "user"
	default:
		okAlpine := ec2RegexpAlpine.FindString(name) != ""
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
			err := fmt.Errorf("unknown ami name, should be one of: \"ami-ID | amzn2 | amzn2023 | deeplearning | bionic | xenial | trusty | focal | jammy | bookworm | trixie | bullseye | buster | stretch | alpine-xx.yy.zz\", got: %s", name)
			Logger.Println("error:", err)
			return "", "", err
		}
	}
	return amiID, sshUser, err
}

func EC2ZonesWithInstance(ctx context.Context, instanceType ec2types.InstanceType) (zones []string, err error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2ZonesWithInstance"}
		d.Start()
		defer d.End()
	}
	out, err := EC2Client().DescribeInstanceTypeOfferings(ctx, &ec2.DescribeInstanceTypeOfferingsInput{
		LocationType: ec2types.LocationTypeAvailabilityZone,
		Filters: []ec2types.Filter{
			{Name: aws.String("instance-type"), Values: []string{string(instanceType)}},
		},
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
		d.Start()
		defer d.End()
	}
	if strings.HasPrefix(sgName, "sg-") {
		return sgName, nil
	}
	vpcID, err := VpcID(ctx, vpcName)
	if err != nil {
		return "", err
	}
	out, err := EC2Client().DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("group-name"), Values: []string{sgName}},
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
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

func EC2Tags(tags []ec2types.Tag) string {
	var res []string
	for _, tag := range tags {
		if !slices.Contains([]string{"Name", "aws:ec2spot:fleet-request-id", "creation-date", "user"}, *tag.Key) {
			res = append(res, fmt.Sprintf("%s=%s", *tag.Key, *tag.Value))
		}
	}
	sort.Strings(res)
	return strings.Join(res, ",")
}

func EC2TagsAll(tags []ec2types.Tag) string {
	var res []string
	for _, tag := range tags {
		if *tag.Key != "Name" {
			res = append(res, fmt.Sprintf("%s=%s", *tag.Key, *tag.Value))
		}
	}
	return strings.Join(res, ",")
}

func EC2Name(tags []ec2types.Tag) string {
	for _, tag := range tags {
		if *tag.Key == "Name" {
			return *tag.Value
		}
	}
	return "-"
}

func EC2NameColored(instance ec2types.Instance) string {
	name := "-"
	for _, tag := range instance.Tags {
		if *tag.Key == "Name" {
			name = *tag.Value
			break
		}
	}
	switch instance.State.Name {
	case ec2types.InstanceStateNameRunning:
		name = Green(name)
	case ec2types.InstanceStateNamePending:
		name = Cyan(name)
	default:
		name = Red(name)
	}
	return name
}

func EC2SecurityGroups(sgs []ec2types.GroupIdentifier) string {
	var res []string
	for _, sg := range sgs {
		if sg.GroupName != nil && *sg.GroupName != "" {
			res = append(res, *sg.GroupName)
		} else if sg.GroupId != nil {
			res = append(res, *sg.GroupId)
		}
	}
	if len(res) == 0 {
		return "-"
	}
	return strings.Join(res, ",")
}

func EC2Kind(instance ec2types.Instance) string {
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
		d.Start()
		defer d.End()
	}
	start := time.Now()
	for {
		now := time.Now()
		allInstances, err := EC2ListInstances(ctx, input.Selectors, "")
		if err != nil {
			return nil, err
		}
		var instances []ec2types.Instance
		var pendingInstances []ec2types.Instance
		for _, instance := range allInstances {
			switch instance.State.Name {
			case ec2types.InstanceStateNameRunning:
				instances = append(instances, instance)
			case ec2types.InstanceStateNamePending:
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
			TimeoutSeconds: 60,
			Instances:      instances,
			Cmd:            "whoami >/dev/null",
			PrivateIP:      input.PrivateIP,
			MaxConcurrency: input.MaxConcurrency,
			Key:            input.Key,
			PrintLock:      sync.Mutex{},
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
			_, err = EC2Client().TerminateInstances(ctx, &ec2.TerminateInstancesInput{
				InstanceIds: terminate,
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
		d.Start()
		defer d.End()
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
		var instances []ec2types.Instance
		var pendingInstances []ec2types.Instance
		for _, instance := range allInstances {
			switch instance.State.Name {
			case ec2types.InstanceStateNameRunning:
				instances = append(instances, instance)
			case ec2types.InstanceStateNamePending:
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
			var ids []string
			for _, instance := range instances {
				ids = append(ids, *instance.InstanceId)
			}
			if len(ids) > 0 {
				_, err := EC2Client().TerminateInstances(context.Background(), &ec2.TerminateInstancesInput{
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
		d.Start()
		defer d.End()
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
	now := time.Now()
	imageName := fmt.Sprintf("%s__%d", name, now.Unix())
	userTag := EC2GetTag(i.Tags, "user", "")
	createdAt := now.UTC().Format(time.RFC3339)

	baseTags := []ec2types.Tag{
		{Key: aws.String("Name"), Value: aws.String(imageName)},
	}
	if userTag != "" {
		baseTags = append(baseTags, ec2types.Tag{Key: aws.String("user"), Value: aws.String(userTag)})
	}
	baseTags = append(baseTags, ec2types.Tag{Key: aws.String("creation-date"), Value: aws.String(createdAt)})

	snapshotTags := []ec2types.Tag{
		{Key: aws.String("Name"), Value: aws.String(imageName)},
	}
	if userTag != "" {
		snapshotTags = append(snapshotTags, ec2types.Tag{Key: aws.String("user"), Value: aws.String(userTag)})
	}
	snapshotTags = append(snapshotTags, ec2types.Tag{Key: aws.String("creation-date"), Value: aws.String(createdAt)})

	image, err := EC2Client().CreateImage(ctx, &ec2.CreateImageInput{
		Name:        aws.String(imageName),
		Description: aws.String(name),
		InstanceId:  i.InstanceId,
		NoReboot:    aws.Bool(false),
		TagSpecifications: []ec2types.TagSpecification{
			{ResourceType: ec2types.ResourceTypeImage, Tags: baseTags},
			{ResourceType: ec2types.ResourceTypeSnapshot, Tags: snapshotTags},
		},
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	if input.Wait {
		start := time.Now()
		for {
			status, err := EC2Client().DescribeImages(ctx, &ec2.DescribeImagesInput{
				ImageIds: []string{aws.ToString(image.ImageId)},
			})
			if err != nil {
				Logger.Println("error:", err)
				return "", err
			}
			if len(status.Images) > 0 && status.Images[0].State == ec2types.ImageStateAvailable {
				break
			}
			Logger.Println("wait for image", fmt.Sprintf("t+%d", int(time.Since(start).Seconds())))
			time.Sleep(1 * time.Second)
		}
	}
	return aws.ToString(image.ImageId), nil
}
func EC2ListSgs(ctx context.Context) ([]ec2types.SecurityGroup, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "EC2ListSgs"}
		d.Start()
		defer d.End()
	}
	var res []ec2types.SecurityGroup
	var token *string
	for {
		out, err := EC2Client().DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
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

func EC2SgRules(p ec2types.IpPermission) ([]EC2SgRule, error) {
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
		d.Start()
		defer d.End()
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
	tags := []ec2types.TagSpecification{{
		ResourceType: ec2types.ResourceTypeSecurityGroup,
		Tags: []ec2types.Tag{
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
			out, err := EC2Client().CreateSecurityGroup(ctx, &ec2.CreateSecurityGroupInput{
				GroupName:         aws.String(input.SgName),
				Description:       aws.String(input.SgName),
				VpcId:             aws.String(vpcID),
				TagSpecifications: tags,
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			sgID = aws.ToString(out.GroupId)
			// wait for security group instantiation
			err = RetryAttempts(ctx, 11, func() error {
				sgs, err := EC2Client().DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
					Filters: []ec2types.Filter{
						{Name: aws.String("group-id"), Values: []string{sgID}},
						{Name: aws.String("vpc-id"), Values: []string{vpcID}},
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
	sgs, err := EC2Client().DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("group-id"), Values: []string{sgID}},
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
		},
	})
	if err != nil && !preview {
		line := strings.Split(err.Error(), "\n")[0]
		if line != "InvalidParameterValue: vpc-id" {
			Logger.Println("error:", err)
			return err
		}
	}
	if sgs == nil {
		sgs = &ec2.DescribeSecurityGroupsOutput{}
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
	existingRules := make(map[EC2SgRule]bool)
	if len(sgs.SecurityGroups) == 1 {
		sg := sgs.SecurityGroups[0]
		for _, r := range sg.IpPermissions {
			rules, err := EC2SgRules(r)
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			for _, rule := range rules {
				existingRules[rule] = true
			}
		}
	}
	desiredRules := make(map[EC2SgRule]bool)
	for _, r := range input.Rules {
		desiredRules[r] = true
	}
	rulesToAdd := []EC2SgRule{}
	for rule := range desiredRules {
		if !existingRules[rule] {
			rulesToAdd = append(rulesToAdd, rule)
		}
	}
	rulesToDelete := []EC2SgRule{}
	for rule := range existingRules {
		if !desiredRules[rule] {
			rulesToDelete = append(rulesToDelete, rule)
		}
	}
	for _, r := range rulesToAdd {
		if !preview {
			sg := sgs.SecurityGroups[0]
			var port *int32
			if r.Port != 0 {
				port = aws.Int32(int32(r.Port))
			}
			proto := "-1" // all ports
			if r.Proto != "" {
				proto = r.Proto
			}
			var ipPermission ec2types.IpPermission
			if strings.HasPrefix(r.Source, "sg-") {
				ipPermission = ec2types.IpPermission{
					UserIdGroupPairs: []ec2types.UserIdGroupPair{{GroupId: aws.String(r.Source)}},
					FromPort:         port,
					ToPort:           port,
					IpProtocol:       aws.String(proto),
				}
			} else {
				ipPermission = ec2types.IpPermission{
					IpRanges:   []ec2types.IpRange{{CidrIp: aws.String(r.Source)}},
					FromPort:   port,
					ToPort:     port,
					IpProtocol: aws.String(proto),
				}
			}
			_, err := EC2Client().AuthorizeSecurityGroupIngress(ctx, &ec2.AuthorizeSecurityGroupIngressInput{
				GroupId:       sg.GroupId,
				IpPermissions: []ec2types.IpPermission{ipPermission},
			})
			if err != nil {
				if !strings.Contains(err.Error(), "InvalidPermission.Duplicate") {
					Logger.Println("error:", err)
					return err
				}
			}
			// wait for rule instantiation
			err = RetryAttempts(ctx, 11, func() error {
				sgs, err := EC2Client().DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
					Filters: []ec2types.Filter{
						{Name: aws.String("group-id"), Values: []string{sgID}},
						{Name: aws.String("vpc-id"), Values: []string{vpcID}},
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
						for _, ruleFound := range rules {
							if ruleFound == r {
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
	for _, k := range rulesToDelete {
		if !preview {
			var port *int32
			if k.Port != 0 {
				port = aws.Int32(int32(k.Port))
			}
			proto := "-1" // all ports
			if k.Proto != "" {
				proto = k.Proto
			}
			sg := sgs.SecurityGroups[0]
			var permission ec2types.IpPermission
			if strings.HasPrefix(k.Source, "sg-") {
				permission = ec2types.IpPermission{
					UserIdGroupPairs: []ec2types.UserIdGroupPair{{GroupId: aws.String(k.Source)}},
					FromPort:         port,
					ToPort:           port,
					IpProtocol:       aws.String(proto),
				}
			} else {
				permission = ec2types.IpPermission{
					IpRanges:   []ec2types.IpRange{{CidrIp: aws.String(k.Source)}},
					FromPort:   port,
					ToPort:     port,
					IpProtocol: aws.String(proto),
				}
			}
			_, err := EC2Client().RevokeSecurityGroupIngress(ctx, &ec2.RevokeSecurityGroupIngressInput{
				GroupId:       sg.GroupId,
				IpPermissions: []ec2types.IpPermission{permission},
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			// wait for rule de-instantiation
			err = RetryAttempts(ctx, 11, func() error {
				sgs, err := EC2Client().DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
					Filters: []ec2types.Filter{
						{Name: aws.String("group-id"), Values: []string{sgID}},
						{Name: aws.String("vpc-id"), Values: []string{vpcID}},
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
						for _, ruleFound := range rules {
							if ruleFound == k {
								return fmt.Errorf("still found rule %#v", ruleFound)
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
		d.Start()
		defer d.End()
	}
	sgID, err := EC2SgID(ctx, vpcName, sgName)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if !preview {
		_, err := EC2Client().DeleteSecurityGroup(ctx, &ec2.DeleteSecurityGroupInput{
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
		d.Start()
		defer d.End()
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
		d.Start()
		defer d.End()
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
		d.Start()
		defer d.End()
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
		d.Start()
		defer d.End()
	}
	out, err := EC2Client().DescribeKeyPairs(ctx, &ec2.DescribeKeyPairsInput{
		KeyNames: []string{keypairName},
	})
	if err != nil {
		if !strings.Contains(err.Error(), "InvalidKeyPair.NotFound") {
			Logger.Println("error:", err)
			return err
		}
	}
	if len(out.KeyPairs) > 0 {
		if !preview {
			_, err := EC2Client().DeleteKeyPair(ctx, &ec2.DeleteKeyPairInput{
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
		d.Start()
		defer d.End()
	}
	pubkey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pubkeyContent))
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	out, err := EC2Client().DescribeKeyPairs(ctx, &ec2.DescribeKeyPairsInput{
		KeyNames: []string{keyName},
	})
	if err != nil {
		if !strings.Contains(err.Error(), "InvalidKeyPair.NotFound") {
			Logger.Println("error:", err)
			return err
		}
	}
	if out == nil {
		out = &ec2.DescribeKeyPairsOutput{}
	}
	tags := []ec2types.TagSpecification{{
		ResourceType: ec2types.ResourceTypeKeyPair,
		Tags: []ec2types.Tag{{
			Key:   aws.String(infraSetTagName),
			Value: aws.String(infraSetName),
		}},
	}}
	switch len(out.KeyPairs) {
	case 0:
		if !preview {
			_, err := EC2Client().ImportKeyPair(ctx, &ec2.ImportKeyPairInput{
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
			remoteFingerprint := strings.TrimSuffix(*out.KeyPairs[0].KeyFingerprint, "=")
			localFingerprint := strings.SplitN(ssh.FingerprintSHA256(pubkey), ":", 2)[1]
			if remoteFingerprint != localFingerprint {
				if !preview {
					_, err := EC2Client().DeleteKeyPair(ctx, &ec2.DeleteKeyPairInput{
						KeyName: aws.String(keyName),
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
					_, err = EC2Client().ImportKeyPair(ctx, &ec2.ImportKeyPairInput{
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
			remoteFingerprint := out.KeyPairs[0].KeyFingerprint
			localFingerprint := strings.TrimRight(stdout.String(), "\n")
			if *remoteFingerprint != localFingerprint {
				if !preview {
					_, err := EC2Client().DeleteKeyPair(ctx, &ec2.DeleteKeyPairInput{
						KeyName: aws.String(keyName),
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
					_, err = EC2Client().ImportKeyPair(ctx, &ec2.ImportKeyPairInput{
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
