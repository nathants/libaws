package ec2

import (
	"context"
	"fmt"
	"github.com/nathants/cli-aws/lib"
	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"

)

func init() {
	lib.Commands["ec2-new"] = new
}

type lsArgs struct {
}

func (lsArgs) Description() string {
	return "\ncreate ec2 instances\n"
}

func new() {
	var args lsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	fmt.Println("yolo")

	numInstances := 1
	name := ""
	sgID := ""
	amiID := ""
	keyName := ""
	instanceTypes := []types.InstanceType{}
	subnetIds := []string{}
	role, err := IAMClient().GetRole(ctx, &iam.GetRoleInput{
		RoleName: aws.String("aws-ec2-spot-fleet-tagging-role"),
	})
	panic1(err)
	launchSpecs := []types.SpotFleetLaunchSpecification{}
	for _, subnetId := range subnetIds {
		for _, instanceType := range instanceTypes {
			launchSpecs = append(launchSpecs, types.SpotFleetLaunchSpecification{
				ImageId:        aws.String(amiID),
				KeyName:        aws.String(keyName),
				SubnetId:       aws.String(subnetId),
				InstanceType:   instanceType,
				SecurityGroups: []types.GroupIdentifier{{GroupId: aws.String(sgID)}},
				BlockDeviceMappings: []types.BlockDeviceMapping{{
					DeviceName: aws.String("/dev/sda1"),
					Ebs: &types.EbsBlockDevice{
						DeleteOnTermination: true,
						Encrypted:           true,
						VolumeType:          "gp3",
					},
				}},
				TagSpecifications: []types.SpotFleetTagSpecification{{
					ResourceType: types.ResourceTypeInstance,
					Tags: []types.Tag{
						{Key: aws.String("Name"), Value: aws.String(name)},
					},
				}},
			})
		}
	}
	spotFleet, err := EC2Client().RequestSpotFleet(ctx, &ec2.RequestSpotFleetInput{SpotFleetRequestConfig: &types.SpotFleetRequestConfigData{
		IamFleetRole:                     role.Role.Arn,
		AllocationStrategy:               types.AllocationStrategyLowestPrice,
		InstanceInterruptionBehavior:     types.InstanceInterruptionBehaviorTerminate,
		ReplaceUnhealthyInstances:        false,
		LaunchSpecifications:             launchSpecs,
		TargetCapacity:                   int32(numInstances),
		Type:                             types.FleetTypeRequest,
		TerminateInstancesWithExpiration: false,
	}})
	panic1(err)
	_ = spotFleet


}
