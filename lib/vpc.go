package lib

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
)

func VpcID(ctx context.Context, name string) (string, error) {
	out, err := EC2Client().DescribeVpcsWithContext(ctx, &ec2.DescribeVpcsInput{
		Filters: []*ec2.Filter{{Name: aws.String("tag:Name"), Values: []*string{aws.String(name)}}},
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	if len(out.Vpcs) != 1 {
		err := fmt.Errorf("didn't find exactly one vpc for name %s: %d", name, len(out.Vpcs))
		Logger.Println("error:", err)
		return "", err
	}
	return *out.Vpcs[0].VpcId, nil
}

func VpcSubnets(ctx context.Context, vpcID string) ([]*ec2.Subnet, error) {
	out, err := EC2Client().DescribeSubnetsWithContext(ctx, &ec2.DescribeSubnetsInput{
		Filters: []*ec2.Filter{{Name: aws.String("vpc-id"), Values: []*string{aws.String(vpcID)}}},
	})
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	return out.Subnets, nil
}
