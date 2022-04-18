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
	if strings.HasPrefix(name, "vpc-") {
		return name, nil
	}
	out, err := EC2Client().DescribeVpcsWithContext(ctx, &ec2.DescribeVpcsInput{
		Filters: []*ec2.Filter{{Name: aws.String("tag:Name"), Values: []*string{aws.String(name)}}},
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	if len(out.Vpcs) != 1 {
		err := fmt.Errorf("%s vpc for name %s: %d", ErrPrefixDidntFindExactlyOne, name, len(out.Vpcs))
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
	if strings.HasPrefix(err.Error(), ErrPrefixDidntFindExactlyOne) {
		return "", err
	}
	tags := []*ec2.Tag{{
		Key:   aws.String("Name"),
		Value: aws.String(name),
	}}
	// create vpc
	cidr := strings.ReplaceAll("10.xx.0.0/16", "xx", fmt.Sprint(xx))
	vpc, err := EC2Client().CreateVpcWithContext(ctx, &ec2.CreateVpcInput{
		CidrBlock: aws.String(cidr),
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	Logger.Println("created:", name, *vpc.Vpc.VpcId, cidr)
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
	// enable dns hostnames
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
	Logger.Println("enabled dns hostnames:", *vpc.Vpc.VpcId)
	// remove all rules from default security group
	out, err := EC2Client().DescribeSecurityGroupsWithContext(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []*ec2.Filter{
			{Name: aws.String("vpc-id"), Values: []*string{vpc.Vpc.VpcId}},
		},
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	if len(out.SecurityGroups) != 1 {
		err := fmt.Errorf("could not find default security group")
		Logger.Println("error:", err)
		return "", err
	}
	securityGroup := out.SecurityGroups[0]
	_, err = EC2Client().RevokeSecurityGroupIngressWithContext(ctx, &ec2.RevokeSecurityGroupIngressInput{
		GroupId:       securityGroup.GroupId,
		IpPermissions: securityGroup.IpPermissions,
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	Logger.Println("removed all rules from default security group:", *securityGroup.GroupId)
	// create and attach internet gateway
	gateway, err := EC2Client().CreateInternetGatewayWithContext(ctx, &ec2.CreateInternetGatewayInput{})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	Logger.Println("created internet gateway:", *gateway.InternetGateway.InternetGatewayId)
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
	Logger.Println("attached internet gateway:", *gateway.InternetGateway.InternetGatewayId)
	// create route to internet
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
	Logger.Println("created route to internet gateway:", *gateway.InternetGateway.InternetGatewayId, *table.RouteTableId)
	// create subnets
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
		subnet, err := EC2Client().CreateSubnetWithContext(ctx, &ec2.CreateSubnetInput{
			VpcId:            vpc.Vpc.VpcId,
			AvailabilityZone: zone.ZoneName,
			CidrBlock:        aws.String(block),
		})
		if err != nil {
			Logger.Println("error:", err)
			return "", err
		}
		Logger.Println("created subnet:", *subnet.Subnet.SubnetId, *zone.ZoneName, block)
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
		Logger.Println("enable map public ip on launch:", *subnet.Subnet.SubnetId)
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

func VpcRm(ctx context.Context, name string) error {
	vpcID, err := VpcID(ctx, name)
	if err != nil {
		if strings.HasPrefix(err.Error(), ErrPrefixDidntFindExactlyOne) {
			return nil
		}
		Logger.Println("error:", err)
		return err
	}
	// fail if vpc has ec2 instances
	instances, err := EC2ListInstances(ctx, []string{vpcID}, "")
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if len(instances) != 0 {
		var ids []string
		for _, instance := range instances {
			ids = append(ids, *instance.InstanceId)
		}
		err := fmt.Errorf("vpc %s has ec2 instances: %v", name, ids)
		Logger.Println("error:", err)
		return err
	}
	// delete internet gateways
	var gateways []*ec2.InternetGateway
	var token *string
	for {
		out, err := EC2Client().DescribeInternetGatewaysWithContext(ctx, &ec2.DescribeInternetGatewaysInput{
			Filters: []*ec2.Filter{{
				Name: aws.String("attachment.vpc-id"), Values: []*string{aws.String(vpcID)},
			}},
			NextToken: token,
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		gateways = append(gateways, out.InternetGateways...)
		if out.NextToken == nil {
			break
		}
		token = out.NextToken
	}
	for _, gateway := range gateways {
		_, err := EC2Client().DetachInternetGatewayWithContext(ctx, &ec2.DetachInternetGatewayInput{
			VpcId:             aws.String(vpcID),
			InternetGatewayId: gateway.InternetGatewayId,
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		_, err = EC2Client().DeleteInternetGatewayWithContext(ctx, &ec2.DeleteInternetGatewayInput{
			InternetGatewayId: gateway.InternetGatewayId,
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		Logger.Println("deleted:", *gateway.InternetGatewayId)
	}
	// delete route tables
	var routeTables []*ec2.RouteTable
	token = nil
	for {
		out, err := EC2Client().DescribeRouteTablesWithContext(ctx, &ec2.DescribeRouteTablesInput{
			Filters: []*ec2.Filter{{
				Name: aws.String("vpc-id"), Values: []*string{aws.String(vpcID)},
			}},
			NextToken: token,
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		routeTables = append(routeTables, out.RouteTables...)
		if out.NextToken == nil {
			break
		}
		out.NextToken = token
	}
	for _, routeTable := range routeTables {
		for _, association := range routeTable.Associations {
			if !*association.Main {
				_, err := EC2Client().DisassociateRouteTableWithContext(ctx, &ec2.DisassociateRouteTableInput{
					AssociationId: association.RouteTableAssociationId,
				})
				if err != nil {
					Logger.Println("error:", err)
					return err
				}
				Logger.Println("deleted:", *association.RouteTableAssociationId)
			}
		}
	}
	// delete vpc endpoints
	token = nil
	var vpcEndpoints []*ec2.VpcEndpoint
	for {
		out, err := EC2Client().DescribeVpcEndpointsWithContext(ctx, &ec2.DescribeVpcEndpointsInput{
			Filters: []*ec2.Filter{{
				Name: aws.String("vpc-id"), Values: []*string{aws.String(vpcID)},
			}},
			NextToken: token,
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		vpcEndpoints = append(vpcEndpoints, out.VpcEndpoints...)
		if out.NextToken == nil {
			break
		}
		token = out.NextToken
	}
	for _, vpcEndpoint := range vpcEndpoints {
		_, err := EC2Client().DeleteVpcEndpointsWithContext(ctx, &ec2.DeleteVpcEndpointsInput{
			VpcEndpointIds: []*string{vpcEndpoint.VpcEndpointId},
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		Logger.Println("deleted:", *vpcEndpoint.VpcEndpointId)
	}
	// delete security groups
	token = nil
	var securityGroups []*ec2.SecurityGroup
	for {
		out, err := EC2Client().DescribeSecurityGroupsWithContext(ctx, &ec2.DescribeSecurityGroupsInput{
			Filters: []*ec2.Filter{{
				Name: aws.String("vpc-id"), Values: []*string{aws.String(vpcID)},
			}},
			NextToken: token,
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		securityGroups = append(securityGroups, out.SecurityGroups...)
		if out.NextToken == nil {
			break
		}
		token = out.NextToken
	}
	for _, securityGroup := range securityGroups {
		if *securityGroup.GroupName == "default" {
			continue // cannot delete default group
		}
		_, err := EC2Client().DeleteSecurityGroupWithContext(ctx, &ec2.DeleteSecurityGroupInput{
			GroupId: securityGroup.GroupId,
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		Logger.Println("deleted:", *securityGroup.GroupId)
	}
	// delete peering connections
	token = nil
	var peeringConnections []*ec2.VpcPeeringConnection
	for {
		out, err := EC2Client().DescribeVpcPeeringConnectionsWithContext(ctx, &ec2.DescribeVpcPeeringConnectionsInput{
			Filters: []*ec2.Filter{{
				Name: aws.String("requester-vpc-info.vpc-id"), Values: []*string{aws.String(vpcID)},
			}},
			NextToken: token,
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		peeringConnections = append(peeringConnections, out.VpcPeeringConnections...)
		if out.NextToken == nil {
			break
		}
		token = out.NextToken
	}
	for _, peeringConnection := range peeringConnections {
		_, err := EC2Client().DeleteVpcPeeringConnectionWithContext(ctx, &ec2.DeleteVpcPeeringConnectionInput{
			VpcPeeringConnectionId: peeringConnection.VpcPeeringConnectionId,
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		Logger.Println("deleted:", *peeringConnection.VpcPeeringConnectionId)
	}
	// delete nacls
	token = nil
	var networkAcls []*ec2.NetworkAcl
	for {
		out, err := EC2Client().DescribeNetworkAclsWithContext(ctx, &ec2.DescribeNetworkAclsInput{
			Filters: []*ec2.Filter{{
				Name: aws.String("vpc-id"), Values: []*string{aws.String(vpcID)},
			}},
			NextToken: token,
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		networkAcls = append(networkAcls, out.NetworkAcls...)
		if out.NextToken == nil {
			break
		}
		token = out.NextToken
	}
	for _, networkAcl := range networkAcls {
		if *networkAcl.IsDefault {
			continue // cannot delete default nacl
		}
		_, err := EC2Client().DeleteNetworkAclWithContext(ctx, &ec2.DeleteNetworkAclInput{
			NetworkAclId: networkAcl.NetworkAclId,
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		Logger.Println("deleted:", *networkAcl.NetworkAclId)
	}
	// delete subnets
	subnets, err := VpcSubnets(ctx, vpcID)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	for _, subnet := range subnets {
		_, err := EC2Client().DeleteSubnetWithContext(ctx, &ec2.DeleteSubnetInput{
			SubnetId: subnet.SubnetId,
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		Logger.Println("deleted:", *subnet.SubnetId)
	}
	// delete vpc
	_, err = EC2Client().DeleteVpcWithContext(ctx, &ec2.DeleteVpcInput{
		VpcId: aws.String(vpcID),
	})
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	Logger.Println("deleted:", name, vpcID)
	return nil
}
