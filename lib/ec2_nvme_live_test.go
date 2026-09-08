package lib

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

func ec2NVMeExample(t *testing.T) (string, string) {
	t.Helper()
	directory := os.Getenv("LIBAWS_EC2_NVME_DIR")
	if directory == "" {
		t.Skip("run through examples/misc/ec2")
	}
	return ec2ExampleUID(t), directory
}

func ec2NVMeVolumes(ctx context.Context, uid string) ([]ec2types.Volume, error) {
	var volumes []ec2types.Volume
	pages := ec2.NewDescribeVolumesPaginator(EC2Client(), &ec2.DescribeVolumesInput{
		Filters: []ec2types.Filter{{Name: aws.String("tag:Name"), Values: []string{"test-ec2-nvme-" + uid}}},
	})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return volumes, err
		}
		volumes = append(volumes, page.Volumes...)
	}
	return volumes, nil
}

// The Python parent registers independent cleanup before requesting this fixture.
// The guest has no ingress or instance credentials; console output carries results.
func TestEC2NVMeExamplePrepare(t *testing.T) {
	uid, directory := ec2NVMeExample(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	volumes, err := ec2NVMeVolumes(ctx, uid)
	if err != nil || len(volumes) != 0 {
		t.Fatalf("fixture volume must initially be absent: %v %v", volumes, err)
	}
	subnets, err := EC2SubnetsFromVpc(ctx, "vpc-test-"+uid, ec2types.InstanceTypeI4iLarge, false)
	if err != nil || len(subnets) != 1 {
		t.Fatalf("select i4i fixture subnet: %v %v", subnets, err)
	}
	out, err := EC2Client().DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{SubnetIds: subnets})
	if err != nil || len(out.Subnets) != 1 {
		t.Fatalf("describe fixture subnet: %v", err)
	}
	volume, err := EC2Client().CreateVolume(ctx, &ec2.CreateVolumeInput{
		AvailabilityZone: out.Subnets[0].AvailabilityZone, Size: aws.Int32(1),
		VolumeType: ec2types.VolumeTypeGp3, Encrypted: aws.Bool(true),
		TagSpecifications: []ec2types.TagSpecification{{ResourceType: ec2types.ResourceTypeVolume,
			Tags: []ec2types.Tag{{Key: aws.String("Name"), Value: aws.String("test-ec2-nvme-" + uid)}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ec2.NewVolumeAvailableWaiter(EC2Client()).Wait(ctx, &ec2.DescribeVolumesInput{VolumeIds: []string{*volume.VolumeId}}, time.Minute); err != nil {
		t.Fatal(err)
	}
	fixture, err := json.Marshal(map[string]string{"volume": *volume.VolumeId, "subnet": subnets[0]})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "fixture.json"), fixture, 0600); err != nil {
		t.Fatal(err)
	}
	init := strings.NewReplacer("@UID@", uid, "@VOLUME@", strings.ReplaceAll(*volume.VolumeId, "-", ""),
		"@INIT@", base64.StdEncoding.EncodeToString([]byte(nvmeInit))).Replace(ec2NVMeExampleInit)
	encoded, err := makeInit(&EC2Config{UserName: "admin", Init: init, SecondsTimeout: 900})
	if err != nil {
		t.Fatal(err)
	}
	userData, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(userData) > 16384 {
		t.Fatalf("fixture exceeds the EC2 user-data limit: %d bytes, %v", len(userData), err)
	}
	if err := os.WriteFile(filepath.Join(directory, "init.sh"), []byte(init), 0600); err != nil {
		t.Fatal(err)
	}
}

const ec2NVMeExampleInit = `
set -euo pipefail
trap 'status=$?; if [ "$status" -ne 0 ]; then printf "LIBAWS_NVME_%s:%s\n" FAIL @UID@ | sudo tee /dev/console; fi' EXIT
work=$(mktemp -d)
printf '%s' '@INIT@' | base64 -d > "$work/init.sh"
store=$(findmnt --raw --noheadings --mountpoint /mnt --output SOURCE)
read -r model < "/sys/block/${store##*/}/device/model"
[ "$model" = 'Amazon EC2 NVMe Instance Storage' ]
[ "$(findmnt --noheadings --mountpoint /mnt --output FSTYPE)" = ext4 ]
echo '@UID@' > /mnt/sentinel
[ "$(cat /mnt/sentinel)" = '@UID@' ]
store_uuid=$(sudo blkid -p -s UUID -o value "$store")

# Wait only for the specifically owned EBS volume attached by the parent.
ebs=""
for ((attempt=0; attempt<120; attempt++)); do
    for sys in /sys/block/nvme*n*; do
        read -r serial < "$sys/device/serial"
        [ "$serial" = '@VOLUME@' ] || continue
        ebs="/dev/${sys##*/}"
        break
    done
    [ -z "$ebs" ] || break
    sleep 1
done
[ -n "$ebs" ] && [ "$ebs" != "$store" ]
read -r model < "/sys/block/${ebs##*/}/device/model"
[ "$model" = 'Amazon Elastic Block Store' ]
layout=$(sudo lsblk --noheadings --raw --output TYPE "$ebs")
[ "$layout" = disk ]
signatures=$(sudo wipefs --no-act --noheadings --output TYPE "$ebs")
[ -z "$signatures" ]
sudo mkfs.ext4 -E nodiscard "$ebs"
mkdir "$work/ebs"
sudo mount "$ebs" "$work/ebs"
printf '%s\n' '@UID@' | sudo tee "$work/ebs/sentinel" >/dev/null
sudo umount "$work/ebs"
before=$(sudo sha256sum "$ebs")

# An existing mounted instance-store filesystem must never be reformatted.
if repeated=$(bash -e "$work/init.sh" 2>&1); then exit 1; fi
[[ "$repeated" == *"$store is mounted"* ]]
[ "$(cat /mnt/sentinel)" = '@UID@' ]

# Reset only the instance-store filesystem just created and verified above.
sudo umount /mnt
sudo awk -v uuid="UUID=$store_uuid" '!($1 == uuid && $2 == "/mnt")' /etc/fstab > "$work/fstab"
sudo cp "$work/fstab" /etc/fstab
sudo wipefs --all "$store"
bash -e "$work/init.sh"
[ "$(findmnt --raw --noheadings --mountpoint /mnt --output SOURCE)" = "$store" ]
echo '@UID@' > /mnt/after
[ "$(cat /mnt/after)" = '@UID@' ]
[ "$(sudo sha256sum "$ebs")" = "$before" ]
sudo mount -o ro,noload "$ebs" "$work/ebs"
[ "$(cat "$work/ebs/sentinel")" = '@UID@' ]
sudo umount "$work/ebs"
printf 'LIBAWS_NVME_%s:%s\n' DONE @UID@ | sudo tee /dev/console
`

func TestEC2NVMeExampleIntegration(t *testing.T) {
	uid, directory := ec2NVMeExample(t)
	data, err := os.ReadFile(filepath.Join(directory, "fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string]string
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	id := os.Getenv("LIBAWS_EC2_NVME_INSTANCE")
	if !ec2RegexpInstanceID.MatchString(id) {
		t.Fatal("parent must provide the owned i4i instance ID")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	instances, err := EC2Client().DescribeInstances(ctx, &ec2.DescribeInstancesInput{InstanceIds: []string{id}})
	if err != nil || len(instances.Reservations) != 1 || len(instances.Reservations[0].Instances) != 1 {
		t.Fatalf("describe owned guest: %v", err)
	}
	instance := instances.Reservations[0].Instances[0]
	if aws.ToString(instance.KeyName) != "test-keypair-"+uid || instance.InstanceType != ec2types.InstanceTypeI4iLarge || instance.InstanceLifecycle != ec2types.InstanceLifecycleTypeSpot || instance.IamInstanceProfile != nil || aws.ToString(instance.SubnetId) != fixture["subnet"] {
		t.Fatal("guest does not belong to the no-credentials i4i Spot fixture")
	}
	volumes, err := EC2Client().DescribeVolumes(ctx, &ec2.DescribeVolumesInput{VolumeIds: []string{fixture["volume"]}})
	if err != nil || len(volumes.Volumes) != 1 || EC2GetTag(volumes.Volumes[0].Tags, "Name", "") != "test-ec2-nvme-"+uid || len(volumes.Volumes[0].Attachments) != 0 {
		t.Fatalf("volume does not belong to the available fixture: %v", err)
	}
	if err := ec2.NewInstanceRunningWaiter(EC2Client()).Wait(ctx, &ec2.DescribeInstancesInput{InstanceIds: []string{id}}, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := EC2Client().AttachVolume(ctx, &ec2.AttachVolumeInput{InstanceId: aws.String(id), VolumeId: aws.String(fixture["volume"]), Device: aws.String("/dev/sdf")}); err != nil {
		t.Fatal(err)
	}
	var console string
	for {
		out, err := EC2Client().GetConsoleOutput(ctx, &ec2.GetConsoleOutputInput{InstanceId: aws.String(id), Latest: aws.Bool(true)})
		if err != nil {
			t.Fatalf("read guest console: %v\n%s", err, console)
		}
		decoded, err := base64.StdEncoding.DecodeString(aws.ToString(out.Output))
		if err != nil {
			t.Fatal(err)
		}
		console = string(decoded)
		if strings.Contains(console, "LIBAWS_NVME_FAIL:"+uid) {
			t.Fatalf("guest assertions failed:\n%s", console)
		}
		if strings.Contains(console, "LIBAWS_NVME_DONE:"+uid) {
			t.Logf("verified %s: writable instance store, used-disk refusal, reinitialization, byte-identical EBS data", id)
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("guest did not finish: %v\n%s", ctx.Err(), console)
		case <-time.After(3 * time.Second):
		}
	}
}

func TestEC2NVMeExampleCleanup(t *testing.T) {
	uid, _ := ec2NVMeExample(t)
	// Volume cleanup must still run if compute cleanup fails. Discovery by the
	// unique fixture tag covers an accepted create that returned no volume ID.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		volumes, err := ec2NVMeVolumes(ctx, uid)
		if err != nil {
			t.Error(err)
		}
		for _, volume := range volumes {
			if _, err := EC2Client().DeleteVolume(ctx, &ec2.DeleteVolumeInput{VolumeId: volume.VolumeId}); err != nil {
				t.Error(err)
				continue
			}
			if err := ec2.NewVolumeDeletedWaiter(EC2Client()).Wait(ctx, &ec2.DescribeVolumesInput{VolumeIds: []string{*volume.VolumeId}}, time.Minute); err != nil {
				t.Error(err)
				continue
			}
			t.Logf("removed owned volume %s", *volume.VolumeId)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cleanupEC2ExampleCompute(t, ctx, "test-keypair-"+uid)
}
