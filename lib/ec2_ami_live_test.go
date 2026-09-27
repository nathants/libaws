package lib

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"
)

func ec2AmiExample(t *testing.T) (string, string) {
	t.Helper()
	uid, directory := os.Getenv("LIBAWS_EC2_AMI_UID"), os.Getenv("LIBAWS_EC2_AMI_DIR")
	if uid == "" || directory == "" {
		t.Skip("run through examples/misc/ec2-ami")
	}
	if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(uid) {
		t.Fatal("requires a unique 12-hex example ID")
	}
	requireLiveAWSAccount(t)
	return uid, directory
}

func ec2AmiFixture(t *testing.T, directory string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(directory, "fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string]string
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

// Registering an empty EBS snapshot is sufficient to test EC2's real image and
// snapshot associations. These fixtures are never booted; no guest is needed.
func TestEC2AmiExamplePrepare(t *testing.T) {
	uid, directory := ec2AmiExample(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	name := "test-ami-" + uid
	filters := []ec2types.Filter{{Name: aws.String("tag:Name"), Values: []string{name}}}
	images, err := EC2Client().DescribeImages(ctx, &ec2.DescribeImagesInput{Owners: []string{"self"}, Filters: filters, IncludeDeprecated: aws.Bool(true), IncludeDisabled: aws.Bool(true)})
	if err != nil || len(images.Images) != 0 {
		t.Fatalf("fixture AMIs must initially be absent: %v", err)
	}
	snapshots, err := EC2Client().DescribeSnapshots(ctx, &ec2.DescribeSnapshotsInput{OwnerIds: []string{"self"}, Filters: filters})
	if err != nil || len(snapshots.Snapshots) != 0 {
		t.Fatalf("fixture snapshots must initially be absent: %v", err)
	}
	volumes, err := EC2Client().DescribeVolumes(ctx, &ec2.DescribeVolumesInput{Filters: filters})
	if err != nil || len(volumes.Volumes) != 0 {
		t.Fatalf("fixture volumes must initially be absent: %v", err)
	}
	zones, err := EC2Client().DescribeAvailabilityZones(ctx, &ec2.DescribeAvailabilityZonesInput{Filters: []ec2types.Filter{
		{Name: aws.String("zone-type"), Values: []string{"availability-zone"}},
		{Name: aws.String("state"), Values: []string{"available"}},
	}})
	if err != nil || len(zones.AvailabilityZones) == 0 {
		t.Fatalf("find volume availability zone: %v", err)
	}
	tags := func(kind ec2types.ResourceType) []ec2types.TagSpecification {
		return []ec2types.TagSpecification{{ResourceType: kind, Tags: []ec2types.Tag{{Key: aws.String("Name"), Value: aws.String(name)}}}}
	}
	volume, err := EC2Client().CreateVolume(ctx, &ec2.CreateVolumeInput{
		AvailabilityZone: zones.AvailabilityZones[0].ZoneName, Size: aws.Int32(1),
		VolumeType: ec2types.VolumeTypeGp3, Encrypted: aws.Bool(true), TagSpecifications: tags(ec2types.ResourceTypeVolume),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("created fixture %s volume %s", uid, *volume.VolumeId)
	if err := ec2.NewVolumeAvailableWaiter(EC2Client()).Wait(ctx, &ec2.DescribeVolumesInput{VolumeIds: []string{*volume.VolumeId}}, 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	fixture := map[string]string{"volume": *volume.VolumeId}
	createSnapshot := func(key, description string) {
		t.Helper()
		out, err := EC2Client().CreateSnapshot(ctx, &ec2.CreateSnapshotInput{
			VolumeId: volume.VolumeId, Description: aws.String(description), TagSpecifications: tags(ec2types.ResourceTypeSnapshot),
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("created %s snapshot %s", key, *out.SnapshotId)
		fixture[key] = *out.SnapshotId
		if err := ec2.NewSnapshotCompletedWaiter(EC2Client()).Wait(ctx, &ec2.DescribeSnapshotsInput{SnapshotIds: []string{*out.SnapshotId}}, 5*time.Minute, func(options *ec2.SnapshotCompletedWaiterOptions) {
			options.MinDelay, options.MaxDelay = time.Second, 3*time.Second
		}); err != nil {
			t.Fatalf("wait for snapshot %s: %v", *out.SnapshotId, err)
		}
	}
	createSnapshot("exclusive", "exclusive fixture without an AMI ID")
	createSnapshot("shared", "shared fixture without an AMI ID")
	for _, key := range []string{"target", "other"} {
		mappings := []ec2types.BlockDeviceMapping{{DeviceName: aws.String("/dev/sda1"), Ebs: &ec2types.EbsBlockDevice{SnapshotId: aws.String(fixture["shared"]), DeleteOnTermination: aws.Bool(true)}}}
		if key == "target" {
			mappings = append(mappings, ec2types.BlockDeviceMapping{DeviceName: aws.String("/dev/sdf"), Ebs: &ec2types.EbsBlockDevice{SnapshotId: aws.String(fixture["exclusive"]), DeleteOnTermination: aws.Bool(true)}})
		}
		image, err := EC2Client().RegisterImage(ctx, &ec2.RegisterImageInput{
			Name: aws.String(name + "-" + key), Architecture: ec2types.ArchitectureValuesX8664,
			VirtualizationType: aws.String("hvm"), RootDeviceName: aws.String("/dev/sda1"),
			BlockDeviceMappings: mappings, TagSpecifications: tags(ec2types.ResourceTypeImage),
		})
		if err != nil {
			t.Fatal(err)
		}
		fixture[key] = *image.ImageId
		t.Logf("registered %s AMI %s", key, *image.ImageId)
		if err := ec2.NewImageAvailableWaiter(EC2Client()).Wait(ctx, &ec2.DescribeImagesInput{ImageIds: []string{*image.ImageId}}, time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	createSnapshot("decoy", "unrelated "+fixture["target"]+" backup")
	data, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "fixture.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestEC2AmiExampleVerifyPreview(t *testing.T) {
	_, directory := ec2AmiExample(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	fixture := ec2AmiFixture(t, directory)
	images, err := EC2Client().DescribeImages(ctx, &ec2.DescribeImagesInput{ImageIds: []string{fixture["target"], fixture["other"]}})
	if err != nil || len(images.Images) != 2 {
		t.Fatalf("preview changed AMIs: %v", err)
	}
	snapshots, err := EC2Client().DescribeSnapshots(ctx, &ec2.DescribeSnapshotsInput{SnapshotIds: []string{fixture["exclusive"], fixture["shared"], fixture["decoy"]}})
	if err != nil || len(snapshots.Snapshots) != 3 {
		t.Fatalf("preview changed snapshots: %v", err)
	}
}

func waitEC2AmiFixtureAbsent(ctx context.Context, kind, id string) error {
	for {
		var err error
		var missing string
		switch kind {
		case "image":
			var out *ec2.DescribeImagesOutput
			out, err = EC2Client().DescribeImages(ctx, &ec2.DescribeImagesInput{ImageIds: []string{id}, IncludeDisabled: aws.Bool(true), IncludeDeprecated: aws.Bool(true)})
			if err == nil && (len(out.Images) == 0 || (len(out.Images) == 1 && out.Images[0].State == ec2types.ImageStateDeregistered)) {
				return nil
			}
			missing = "InvalidAMIID.NotFound"
		case "snapshot":
			_, err = EC2Client().DescribeSnapshots(ctx, &ec2.DescribeSnapshotsInput{SnapshotIds: []string{id}})
			missing = "InvalidSnapshot.NotFound"
		default:
			return fmt.Errorf("unknown fixture kind: %s", kind)
		}
		var apiError smithy.APIError
		if errors.As(err, &apiError) && apiError.ErrorCode() == missing {
			return nil
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for %s %s absence: %w", kind, id, ctx.Err())
		case <-time.After(time.Second):
		}
	}
}

func TestEC2AmiExampleVerify(t *testing.T) {
	_, directory := ec2AmiExample(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	fixture := ec2AmiFixture(t, directory)
	if err := waitEC2AmiFixtureAbsent(ctx, "image", fixture["target"]); err != nil {
		t.Fatal(err)
	}
	if err := waitEC2AmiFixtureAbsent(ctx, "snapshot", fixture["exclusive"]); err != nil {
		t.Fatal(err)
	}
	images, err := EC2Client().DescribeImages(ctx, &ec2.DescribeImagesInput{ImageIds: []string{fixture["other"]}, IncludeDisabled: aws.Bool(true), IncludeDeprecated: aws.Bool(true)})
	if err != nil || len(images.Images) != 1 || images.Images[0].State != ec2types.ImageStateAvailable || len(images.Images[0].BlockDeviceMappings) != 1 || aws.ToString(images.Images[0].BlockDeviceMappings[0].Ebs.SnapshotId) != fixture["shared"] {
		t.Fatalf("shared AMI was changed: %v", err)
	}
	snapshots, err := EC2Client().DescribeSnapshots(ctx, &ec2.DescribeSnapshotsInput{SnapshotIds: []string{fixture["shared"], fixture["decoy"]}})
	if err != nil || len(snapshots.Snapshots) != 2 {
		t.Fatalf("shared or unrelated snapshot was deleted: %v", err)
	}
	for _, snapshot := range snapshots.Snapshots {
		if snapshot.State != ec2types.SnapshotStateCompleted {
			t.Fatalf("surviving snapshot is not usable: %s", aws.ToString(snapshot.SnapshotId))
		}
	}
	t.Logf("removed %s and exclusive %s; preserved shared %s, other AMI %s, and unrelated %s", fixture["target"], fixture["exclusive"], fixture["shared"], fixture["other"], fixture["decoy"])
}

func TestEC2AmiExampleCleanup(t *testing.T) {
	uid, _ := ec2AmiExample(t)
	filters := []ec2types.Filter{{Name: aws.String("tag:Name"), Values: []string{"test-ami-" + uid}}}
	// Discover each root independently by its unique creation-time tag, including
	// partial creates. Collect pages before mutation; one failed phase must not
	// consume another phase's cleanup deadline.
	t.Run("images", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		var images []ec2types.Image
		pages := ec2.NewDescribeImagesPaginator(EC2Client(), &ec2.DescribeImagesInput{Owners: []string{"self"}, Filters: filters, IncludeDisabled: aws.Bool(true), IncludeDeprecated: aws.Bool(true)})
		for pages.HasMorePages() {
			page, err := pages.NextPage(ctx)
			if err != nil {
				t.Error(err)
				break
			}
			images = append(images, page.Images...)
		}
		for _, image := range images {
			if image.State == ec2types.ImageStateDeregistered {
				continue
			}
			if _, err := EC2Client().DeregisterImage(ctx, &ec2.DeregisterImageInput{ImageId: image.ImageId}); err != nil {
				t.Error(err)
				continue
			}
			t.Logf("deregistered fixture AMI %s", *image.ImageId)
		}
		for _, image := range images {
			if err := waitEC2AmiFixtureAbsent(ctx, "image", *image.ImageId); err != nil {
				t.Error(err)
			}
		}
	})
	t.Run("snapshots", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		var snapshots []ec2types.Snapshot
		pages := ec2.NewDescribeSnapshotsPaginator(EC2Client(), &ec2.DescribeSnapshotsInput{OwnerIds: []string{"self"}, Filters: filters})
		for pages.HasMorePages() {
			page, err := pages.NextPage(ctx)
			if err != nil {
				t.Error(err)
				break
			}
			snapshots = append(snapshots, page.Snapshots...)
		}
		for _, snapshot := range snapshots {
			if _, err := EC2Client().DeleteSnapshot(ctx, &ec2.DeleteSnapshotInput{SnapshotId: snapshot.SnapshotId}); err != nil {
				t.Error(err)
				continue
			}
			t.Logf("deleted fixture snapshot %s", *snapshot.SnapshotId)
		}
		for _, snapshot := range snapshots {
			if err := waitEC2AmiFixtureAbsent(ctx, "snapshot", *snapshot.SnapshotId); err != nil {
				t.Error(err)
			}
		}
	})
	t.Run("volumes", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
		defer cancel()
		var volumes []ec2types.Volume
		pages := ec2.NewDescribeVolumesPaginator(EC2Client(), &ec2.DescribeVolumesInput{Filters: filters})
		for pages.HasMorePages() {
			page, err := pages.NextPage(ctx)
			if err != nil {
				t.Error(err)
				break
			}
			volumes = append(volumes, page.Volumes...)
		}
		for _, volume := range volumes {
			if len(volume.Attachments) != 0 || !slices.Contains([]ec2types.VolumeState{ec2types.VolumeStateAvailable, ec2types.VolumeStateCreating, ec2types.VolumeStateDeleting}, volume.State) {
				t.Errorf("unexpectedly attached fixture volume %s", aws.ToString(volume.VolumeId))
				continue
			}
			if volume.State == ec2types.VolumeStateCreating {
				if err := ec2.NewVolumeAvailableWaiter(EC2Client()).Wait(ctx, &ec2.DescribeVolumesInput{VolumeIds: []string{*volume.VolumeId}}, time.Minute); err != nil {
					t.Error(err)
					continue
				}
			}
			if volume.State != ec2types.VolumeStateDeleting {
				if _, err := EC2Client().DeleteVolume(ctx, &ec2.DeleteVolumeInput{VolumeId: volume.VolumeId}); err != nil {
					t.Error(err)
					continue
				}
				t.Logf("requested deletion of fixture volume %s", *volume.VolumeId)
			}
			if err := ec2.NewVolumeDeletedWaiter(EC2Client()).Wait(ctx, &ec2.DescribeVolumesInput{VolumeIds: []string{*volume.VolumeId}}, 8*time.Minute); err != nil {
				t.Errorf("wait for volume %s deletion: %v", *volume.VolumeId, err)
			} else {
				t.Logf("deleted fixture volume %s", *volume.VolumeId)
			}
		}
	})
}

// A public AMI is visible to every account and owned by none of them. Deleting
// it must fail before any mutation; no fixture is created and no guest runs.
func TestEC2AmiExampleRejectsForeignOwner(t *testing.T) {
	ec2AmiExample(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	images, err := EC2Client().DescribeImages(ctx, &ec2.DescribeImagesInput{
		Owners: []string{"amazon"},
		Filters: []ec2types.Filter{
			{Name: aws.String("state"), Values: []string{"available"}},
			{Name: aws.String("architecture"), Values: []string{"x86_64"}},
			{Name: aws.String("root-device-type"), Values: []string{"ebs"}},
		},
		MaxResults: aws.Int32(5),
	})
	if err != nil || len(images.Images) == 0 {
		t.Fatalf("find an amazon-owned public AMI: %v", err)
	}
	foreign := aws.ToString(images.Images[0].ImageId)
	if !ec2RegexpAmiID.MatchString(foreign) || aws.ToString(images.Images[0].OwnerId) == "" {
		t.Fatalf("invalid public AMI fixture: %q", foreign)
	}
	for _, preview := range []bool{true, false} {
		if err := EC2DeleteAmi(ctx, foreign, preview); err == nil || !strings.Contains(err.Error(), "not owned") {
			t.Fatalf("foreign AMI was not refused: %v", err)
		}
	}
	check, err := EC2Client().DescribeImages(ctx, &ec2.DescribeImagesInput{ImageIds: []string{foreign}})
	if err != nil || len(check.Images) != 1 || check.Images[0].State != ec2types.ImageStateAvailable || aws.ToString(check.Images[0].OwnerId) == "" {
		t.Fatalf("foreign AMI was mutated: %v", err)
	}
	t.Logf("refused to delete %s owned by %s without mutation", foreign, aws.ToString(check.Images[0].OwnerId))
}
