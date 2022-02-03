package lib

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
)

func VpcList(ctx context.Context) ([]*ec2.Vpc, error) {
	var token *string
	var res []*ec2.Vpc
	for {
		out, err := EC2Client().DescribeVpcsWithContext(ctx, &ec2.DescribeVpcsInput{
			NextToken: token,
		})
		if err != nil {
			return nil, err
		}
		res = append(res, out.Vpcs...)
		if out.NextToken == nil {
			break
		}
		token = out.NextToken
	}
	return res, nil
}

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

func VpcEnsure(ctx context.Context, name string, xx int) (string, error) {
	id, err := VpcID(ctx, name)
	if err == nil {
		// TODO assert vpc state
		return id, nil
	}
	tags := []*ec2.Tag{{
		Key:   aws.String("Name"),
		Value: aws.String(name),
	}}
	cidr := strings.ReplaceAll("10.xx.0.0/16", "xx", fmt.Sprint(xx))
	Logger.Println("cidr:", cidr)
	vpc, err := EC2Client().CreateVpcWithContext(ctx, &ec2.CreateVpcInput{
		CidrBlock: aws.String(cidr),
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	err = Retry(ctx, func() error {
		_, err := EC2Client().CreateTagsWithContext(ctx, &ec2.CreateTagsInput{
			Resources: []*string{vpc.Vpc.VpcId},
			Tags:      tags,
		})
		return err
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	err = EC2Client().WaitUntilVpcAvailableWithContext(ctx, &ec2.DescribeVpcsInput{
		VpcIds: []*string{vpc.Vpc.VpcId},
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	err = Retry(ctx, func() error {
		_, err := EC2Client().ModifyVpcAttributeWithContext(ctx, &ec2.ModifyVpcAttributeInput{
			VpcId: vpc.Vpc.VpcId,
			EnableDnsHostnames: &ec2.AttributeBooleanValue{
				Value: aws.Bool(true),
			},
		})
		return err
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	gateway, err := EC2Client().CreateInternetGatewayWithContext(ctx, &ec2.CreateInternetGatewayInput{})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	err = Retry(ctx, func() error {
		_, err := EC2Client().CreateTagsWithContext(ctx, &ec2.CreateTagsInput{
			Resources: []*string{gateway.InternetGateway.InternetGatewayId},
			Tags:      tags,
		})
		return err
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	err = Retry(ctx, func() error {
		_, err := EC2Client().AttachInternetGatewayWithContext(ctx, &ec2.AttachInternetGatewayInput{
			VpcId:             vpc.Vpc.VpcId,
			InternetGatewayId: gateway.InternetGateway.InternetGatewayId,
		})
		return err
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	routes, err := EC2Client().DescribeRouteTablesWithContext(ctx, &ec2.DescribeRouteTablesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []*string{vpc.Vpc.VpcId},
			},
		},
	})
	if err != nil {
		return "", err
	}
	if len(routes.RouteTables) != 1 {
		err := fmt.Errorf("needed exactly 1 route table %s", Pformat(routes.RouteTables))
		Logger.Println("error:", err)
		return "", err
	}
	table := routes.RouteTables[0]
	_, err = EC2Client().CreateRouteWithContext(ctx, &ec2.CreateRouteInput{
		DestinationCidrBlock: aws.String("0.0.0.0/0"),
		GatewayId:            gateway.InternetGateway.InternetGatewayId,
		RouteTableId:         table.RouteTableId,
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	zones, err := Zones(ctx)
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	for i, zone := range zones {
		str := strings.Split(cidr, "/")[0]
		slice := strings.Split(str, ".")[:2]
		slice = append(slice, fmt.Sprint(16*i+1))
		slice = append(slice, "0/20")
		block := strings.Join(slice, ".")
		Logger.Println("block:", block, "zone:", zone)
		subnet, err := EC2Client().CreateSubnetWithContext(ctx, &ec2.CreateSubnetInput{
			VpcId:            vpc.Vpc.VpcId,
			AvailabilityZone: zone.ZoneName,
			CidrBlock:        aws.String(block),
		})
		if err != nil {
			Logger.Println("error:", err)
			return "", err
		}
		err = Retry(ctx, func() error {
			_, err := EC2Client().CreateTagsWithContext(ctx, &ec2.CreateTagsInput{
				Resources: []*string{subnet.Subnet.SubnetId},
				Tags:      tags,
			})
			return err
		})
		if err != nil {
			Logger.Println("error:", err)
			return "", err
		}
		err = Retry(ctx, func() error {
			_, err := EC2Client().ModifySubnetAttributeWithContext(ctx, &ec2.ModifySubnetAttributeInput{
				SubnetId:            subnet.Subnet.SubnetId,
				MapPublicIpOnLaunch: &ec2.AttributeBooleanValue{Value: aws.Bool(true)},
			})
			return err
		})
		if err != nil {
			Logger.Println("error:", err)
			return "", err
		}
		err = Retry(ctx, func() error {
			_, err := EC2Client().AssociateRouteTableWithContext(ctx, &ec2.AssociateRouteTableInput{
				RouteTableId: table.RouteTableId,
				SubnetId:     subnet.Subnet.SubnetId,
			})
			return err
		})
		if err != nil {
			Logger.Println("error:", err)
			return "", err
		}
	}
	return *vpc.Vpc.VpcId, nil
}
