package lib

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// The official Debian catalog lists backports-kernel images beside the standard
// images under the same release prefix. The selected base must be the newest
// standard image, never a backports image.
func TestEC2AmiBaseDebianLiveCatalog(t *testing.T) {
	requireLiveAWSAccount(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, arch := range []string{EC2ArchAmd64, EC2ArchArm64} {
		t.Run(arch, func(t *testing.T) {
			out, err := EC2Client().DescribeImages(ctx, &ec2.DescribeImagesInput{
				Owners: []string{"136693071363"},
				Filters: []ec2types.Filter{
					{Name: aws.String("name"), Values: []string{"debian-12-*"}},
					{Name: aws.String("architecture"), Values: []string{arch}},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			standard := regexp.MustCompile(`^debian-12-` + debianArches[arch] + `-[0-9]{8}-[0-9]+$`)
			var newest string
			for _, image := range out.Images {
				if standard.MatchString(aws.ToString(image.Name)) && aws.ToString(image.CreationDate) > newest {
					newest = aws.ToString(image.CreationDate)
				}
			}
			if newest == "" {
				t.Fatalf("catalog lists no standard Debian 12 %s image among %d images", arch, len(out.Images))
			}
			id, user, err := EC2AmiBase(ctx, EC2AmiDebianBookworm, arch)
			if err != nil || user != "admin" {
				t.Fatalf("select base image: user %q error %v", user, err)
			}
			for _, image := range out.Images {
				if aws.ToString(image.ImageId) != id {
					continue
				}
				if !standard.MatchString(aws.ToString(image.Name)) || aws.ToString(image.CreationDate) != newest {
					t.Fatalf("selected %s %s created %s, want the newest standard image created %s", id, aws.ToString(image.Name), aws.ToString(image.CreationDate), newest)
				}
				return
			}
			t.Fatalf("selected %s is not an official Debian 12 %s image", id, arch)
		})
	}
}
