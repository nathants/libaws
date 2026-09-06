package lib

import (
	"context"
	"fmt"
	"math/rand"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// EC2SubnetsFromVpc selects subnets in zones offering the requested instance type.
// Spot uses every eligible VPC subnet; on-demand uses one at random.
func EC2SubnetsFromVpc(ctx context.Context, vpcName string, instanceType ec2types.InstanceType, spot bool) ([]string, error) {
	vpcID, err := VpcID(ctx, vpcName)
	if err != nil {
		return nil, err
	}
	zones, err := EC2ZonesWithInstance(ctx, instanceType)
	if err != nil {
		return nil, err
	}
	subnets, err := VpcSubnets(ctx, vpcID)
	if err != nil {
		return nil, err
	}
	var subnetIDs []string
	for _, subnet := range subnets {
		if slices.Contains(zones, aws.ToString(subnet.AvailabilityZone)) {
			subnetIDs = append(subnetIDs, aws.ToString(subnet.SubnetId))
		}
	}
	if len(subnetIDs) == 0 {
		return nil, fmt.Errorf("no subnets for vpc %s with instance type %s", vpcID, instanceType)
	}
	if !spot {
		subnetIDs = []string{subnetIDs[rand.Intn(len(subnetIDs))]}
	}
	return subnetIDs, nil
}
