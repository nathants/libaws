package lib

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"
)

var ec2RegexpAmiID = regexp.MustCompile(`^ami-([0-9a-f]{8}|[0-9a-f]{17})$`)
var ec2RegexpSnapshotID = regexp.MustCompile(`^snap-([0-9a-f]{8}|[0-9a-f]{17})$`)

// EC2DeleteAmi deregisters an owned AMI and deletes its exclusively associated
// snapshots. AWS retains snapshots associated with another AMI at deletion time.
func EC2DeleteAmi(ctx context.Context, amiID string, preview bool) error {
	if !ec2RegexpAmiID.MatchString(amiID) {
		return fmt.Errorf("invalid AMI ID: %q", amiID)
	}
	images, err := EC2Client().DescribeImages(ctx, &ec2.DescribeImagesInput{
		ImageIds: []string{amiID}, Owners: []string{"self"},
		IncludeDeprecated: aws.Bool(true), IncludeDisabled: aws.Bool(true),
	})
	if err != nil {
		var apiError smithy.APIError
		if errors.As(err, &apiError) && apiError.ErrorCode() == "InvalidAMIID.NotFound" {
			return nil
		}
		return err
	}
	if aws.ToString(images.NextToken) != "" || len(images.Images) > 1 {
		return fmt.Errorf("incomplete or ambiguous AMI lookup for %s", amiID)
	}
	if len(images.Images) == 0 {
		return nil
	}
	image := images.Images[0]
	if aws.ToString(image.ImageId) != amiID || aws.ToString(image.OwnerId) == "" {
		return fmt.Errorf("invalid owned AMI metadata for %s", amiID)
	}
	if image.State == ec2types.ImageStateDeregistered {
		return nil
	}
	snapshots := make(map[string]bool)
	var ids []string
	for _, mapping := range image.BlockDeviceMappings {
		if mapping.Ebs == nil {
			continue
		}
		id := aws.ToString(mapping.Ebs.SnapshotId)
		if id == "" {
			continue // An AMI can also declare a new empty EBS volume.
		}
		if !ec2RegexpSnapshotID.MatchString(id) {
			return fmt.Errorf("invalid backing snapshot for %s: %q", amiID, id)
		}
		if !snapshots[id] {
			snapshots[id] = true
			ids = append(ids, id)
		}
	}
	if preview {
		Logger.Println(PreviewString(true)+"deregister:", amiID)
		for _, id := range ids {
			Logger.Println(PreviewString(true)+"delete backing snapshot if not shared:", id)
		}
		return nil
	}
	// Delegate association and sharing checks to EC2 at the mutation boundary;
	// a client-side scan followed by DeleteSnapshot would race other AMI changes.
	result, err := EC2Client().DeregisterImage(ctx, &ec2.DeregisterImageInput{
		ImageId: aws.String(amiID), DeleteAssociatedSnapshots: aws.Bool(true),
	})
	if err != nil {
		return err
	}
	if !aws.ToBool(result.Return) {
		return fmt.Errorf("AMI deregistration was not confirmed: %s", amiID)
	}
	Logger.Println("deregistered:", amiID)
	seen := make(map[string]bool)
	var failures []error
	for _, deleted := range result.DeleteSnapshotResults {
		id := aws.ToString(deleted.SnapshotId)
		if !snapshots[id] || seen[id] {
			failures = append(failures, fmt.Errorf("unexpected snapshot deletion result for %s: %q", amiID, id))
			continue
		}
		seen[id] = true
		switch deleted.ReturnCode {
		case ec2types.SnapshotReturnCodesSuccess:
			Logger.Println("deleted backing snapshot:", id)
		case ec2types.SnapshotReturnCodesWarnSkipped:
			Logger.Println("preserved shared snapshot:", id)
		default:
			failures = append(failures, fmt.Errorf("delete backing snapshot %s after deregistering %s: %s", id, amiID, deleted.ReturnCode))
		}
	}
	for _, id := range ids {
		if !seen[id] {
			failures = append(failures, fmt.Errorf("missing snapshot deletion result for %s after deregistering %s", id, amiID))
		}
	}
	return errors.Join(failures...)
}
