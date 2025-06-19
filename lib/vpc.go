package lib

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

func VpcList(ctx context.Context) ([]ec2types.Vpc, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "VpcList"}
		d.Start()
		defer d.End()
	}
	var token *string
	var res []ec2types.Vpc
	for {
		out, err := EC2Client().DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
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

func VpcListSubnets(ctx context.Context, vpcID string) ([]ec2types.Subnet, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "VpcListSubnets"}
		d.Start()
		defer d.End()
	}
	input := &ec2.DescribeSubnetsInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []string{vpcID},
			},
		},
	}
	out, err := EC2Client().DescribeSubnets(ctx, input)
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	return out.Subnets, nil
}

func VpcID(ctx context.Context, name string) (string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "VpcID"}
		d.Start()
		defer d.End()
	}
	if strings.HasPrefix(name, "vpc-") {
		return name, nil
	}
	out, err := EC2Client().DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("tag:Name"),
				Values: []string{name},
			},
		},
	})
	if err != nil {
		Logger.Println("error:", err)
		return "", err
	}
	if len(out.Vpcs) != 1 {
		err := fmt.Errorf("%s vpc for name %s: %d", ErrPrefixDidntFindExactlyOne, name, len(out.Vpcs))
		return "", err
	}
	return aws.ToString(out.Vpcs[0].VpcId), nil
}

func VpcSubnets(ctx context.Context, vpcID string) ([]ec2types.Subnet, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "VpcSubnets"}
		d.Start()
		defer d.End()
	}
	out, err := EC2Client().DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []string{vpcID},
			},
		},
	})
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	return out.Subnets, nil
}

func VpcEnsure(ctx context.Context, infraSetName, vpcName string, preview bool) (string, error) {
	// TODO make this idempotent and previewable. currently if aborted must rm then ensure.
	if doDebug {
		d := &Debug{start: time.Now(), name: "VpcEnsure"}
		d.Start()
		defer d.End()
	}
	xx := 0
	id, err := VpcID(ctx, vpcName)
	if err == nil {
		return id, nil
	}
	if !preview {
		if !strings.HasPrefix(err.Error(), ErrPrefixDidntFindExactlyOne) {
			Logger.Println("error:", err)
			return "", err
		}
		tags := []ec2types.Tag{
			{
				Key:   aws.String("Name"),
				Value: aws.String(vpcName),
			},
			{
				Key:   aws.String(infraSetTagName),
				Value: aws.String(infraSetName),
			},
		}
		// create vpc
		cidr := strings.ReplaceAll("10.xx.0.0/16", "xx", fmt.Sprint(xx))
		vpc, err := EC2Client().CreateVpc(ctx, &ec2.CreateVpcInput{
			CidrBlock: aws.String(cidr),
			TagSpecifications: []ec2types.TagSpecification{
				{
					ResourceType: ec2types.ResourceTypeVpc,
					Tags:         tags,
				},
			},
		})
		if err != nil {
			Logger.Println("error:", err)
			return "", err
		}
		Logger.Println("created:", vpcName, aws.ToString(vpc.Vpc.VpcId), cidr)
		err = ec2.NewVpcAvailableWaiter(EC2Client()).Wait(ctx, &ec2.DescribeVpcsInput{
			VpcIds: []string{aws.ToString(vpc.Vpc.VpcId)},
		}, 10*time.Minute)
		if err != nil {
			Logger.Println("error:", err)
			return "", err
		}
		// enable dns hostnames
		err = Retry(ctx, func() error {
			_, err := EC2Client().ModifyVpcAttribute(ctx, &ec2.ModifyVpcAttributeInput{
				VpcId: vpc.Vpc.VpcId,
				EnableDnsHostnames: &ec2types.AttributeBooleanValue{
					Value: aws.Bool(true),
				},
			})
			return err
		})
		if err != nil {
			Logger.Println("error:", err)
			return "", err
		}
		Logger.Println("enabled dns hostnames:", aws.ToString(vpc.Vpc.VpcId))
		// remove all rules from default security group
		out, err := EC2Client().DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
			Filters: []ec2types.Filter{
				{
					Name:   aws.String("vpc-id"),
					Values: []string{aws.ToString(vpc.Vpc.VpcId)},
				},
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
		_, err = EC2Client().RevokeSecurityGroupIngress(ctx, &ec2.RevokeSecurityGroupIngressInput{
			GroupId:       securityGroup.GroupId,
			IpPermissions: securityGroup.IpPermissions,
		})
		if err != nil {
			Logger.Println("error:", err)
			return "", err
		}
		Logger.Println("removed all rules from default security group:", aws.ToString(securityGroup.GroupId))
		// create and attach internet gateway
		gateway, err := EC2Client().CreateInternetGateway(ctx, &ec2.CreateInternetGatewayInput{
			TagSpecifications: []ec2types.TagSpecification{
				{
					ResourceType: ec2types.ResourceTypeInternetGateway,
					Tags:         tags,
				},
			},
		})
		if err != nil {
			Logger.Println("error:", err)
			return "", err
		}
		Logger.Println("created internet gateway:", aws.ToString(gateway.InternetGateway.InternetGatewayId))
		err = Retry(ctx, func() error {
			_, err := EC2Client().AttachInternetGateway(ctx, &ec2.AttachInternetGatewayInput{
				VpcId:             vpc.Vpc.VpcId,
				InternetGatewayId: gateway.InternetGateway.InternetGatewayId,
			})
			return err
		})
		if err != nil {
			Logger.Println("error:", err)
			return "", err
		}
		Logger.Println("attached internet gateway:", aws.ToString(gateway.InternetGateway.InternetGatewayId))
		// create route to internet
		routes, err := EC2Client().DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{
			Filters: []ec2types.Filter{
				{
					Name:   aws.String("vpc-id"),
					Values: []string{aws.ToString(vpc.Vpc.VpcId)},
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
		_, err = EC2Client().CreateRoute(ctx, &ec2.CreateRouteInput{
			DestinationCidrBlock: aws.String("0.0.0.0/0"),
			GatewayId:            gateway.InternetGateway.InternetGatewayId,
			RouteTableId:         table.RouteTableId,
		})
		if err != nil {
			Logger.Println("error:", err)
			return "", err
		}
		Logger.Println("created route to internet gateway:", aws.ToString(gateway.InternetGateway.InternetGatewayId), aws.ToString(table.RouteTableId))
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
			subnet, err := EC2Client().CreateSubnet(ctx, &ec2.CreateSubnetInput{
				VpcId:            vpc.Vpc.VpcId,
				AvailabilityZone: zone.ZoneName,
				CidrBlock:        aws.String(block),
				TagSpecifications: []ec2types.TagSpecification{
					{
						ResourceType: ec2types.ResourceTypeSubnet,
						Tags:         tags,
					},
				},
			})
			if err != nil {
				Logger.Println("error:", err)
				return "", err
			}
			Logger.Println("created subnet:", aws.ToString(subnet.Subnet.SubnetId), aws.ToString(zone.ZoneName), block)
			err = Retry(ctx, func() error {
				_, err := EC2Client().ModifySubnetAttribute(ctx, &ec2.ModifySubnetAttributeInput{
					SubnetId:            subnet.Subnet.SubnetId,
					MapPublicIpOnLaunch: &ec2types.AttributeBooleanValue{Value: aws.Bool(true)},
				})
				return err
			})
			if err != nil {
				Logger.Println("error:", err)
				return "", err
			}
			Logger.Println("enable map public ip on launch:", aws.ToString(subnet.Subnet.SubnetId))
			err = Retry(ctx, func() error {
				_, err := EC2Client().AssociateRouteTable(ctx, &ec2.AssociateRouteTableInput{
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
		return aws.ToString(vpc.Vpc.VpcId), nil
	}
	Logger.Println(PreviewString(preview)+"created vpc:", vpcName)
	return "", nil
}

func VpcRm(ctx context.Context, name string, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "VpcRm"}
		d.Start()
		defer d.End()
	}
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
			if instance.State.Name != ec2types.InstanceStateNameTerminated {
				ids = append(ids, *instance.InstanceId)
			}
		}
		if len(ids) > 0 {
			err := fmt.Errorf("vpc %s has ec2 instances: %v", name, ids)
			Logger.Println("error:", err)
			return err
		}
	}
	// delete internet gateways
	var gateways []ec2types.InternetGateway
	var token *string
	for {
		out, err := EC2Client().DescribeInternetGateways(ctx, &ec2.DescribeInternetGatewaysInput{
			Filters: []ec2types.Filter{
				{
					Name:   aws.String("attachment.vpc-id"),
					Values: []string{vpcID},
				},
			},
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
		if !preview {
			_, err := EC2Client().DetachInternetGateway(ctx, &ec2.DetachInternetGatewayInput{
				VpcId:             aws.String(vpcID),
				InternetGatewayId: gateway.InternetGatewayId,
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
			_, err = EC2Client().DeleteInternetGateway(ctx, &ec2.DeleteInternetGatewayInput{
				InternetGatewayId: gateway.InternetGatewayId,
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"deleted:", aws.ToString(gateway.InternetGatewayId))
	}
	// delete route tables
	var routeTables []ec2types.RouteTable
	token = nil
	for {
		out, err := EC2Client().DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{
			Filters: []ec2types.Filter{
				{
					Name:   aws.String("vpc-id"),
					Values: []string{vpcID},
				},
			},
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
		token = out.NextToken
	}
	for _, routeTable := range routeTables {
		for _, association := range routeTable.Associations {
			if association.Main != nil && !*association.Main {
				if !preview {
					_, err := EC2Client().DisassociateRouteTable(ctx, &ec2.DisassociateRouteTableInput{
						AssociationId: association.RouteTableAssociationId,
					})
					if err != nil {
						Logger.Println("error:", err)
						return err
					}
				}
				Logger.Println(PreviewString(preview)+"deleted:", aws.ToString(association.RouteTableAssociationId))
			}
		}
	}
	// delete vpc endpoints
	token = nil
	var vpcEndpoints []ec2types.VpcEndpoint
	for {
		out, err := EC2Client().DescribeVpcEndpoints(ctx, &ec2.DescribeVpcEndpointsInput{
			Filters: []ec2types.Filter{
				{
					Name:   aws.String("vpc-id"),
					Values: []string{vpcID},
				},
			},
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
		if !preview {
			_, err := EC2Client().DeleteVpcEndpoints(ctx, &ec2.DeleteVpcEndpointsInput{
				VpcEndpointIds: []string{aws.ToString(vpcEndpoint.VpcEndpointId)},
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"deleted:", aws.ToString(vpcEndpoint.VpcEndpointId))
	}
	// delete security groups
	token = nil
	var securityGroups []ec2types.SecurityGroup
	for {
		out, err := EC2Client().DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
			Filters: []ec2types.Filter{
				{
					Name:   aws.String("vpc-id"),
					Values: []string{vpcID},
				},
			},
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
		if securityGroup.GroupName != nil && *securityGroup.GroupName == "default" {
			continue // cannot delete default group
		}
		if !preview {
			_, err := EC2Client().DeleteSecurityGroup(ctx, &ec2.DeleteSecurityGroupInput{
				GroupId: securityGroup.GroupId,
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"deleted:", aws.ToString(securityGroup.GroupId))
	}
	// delete peering connections
	token = nil
	var peeringConnections []ec2types.VpcPeeringConnection
	for {
		out, err := EC2Client().DescribeVpcPeeringConnections(ctx, &ec2.DescribeVpcPeeringConnectionsInput{
			Filters: []ec2types.Filter{
				{
					Name:   aws.String("requester-vpc-info.vpc-id"),
					Values: []string{vpcID},
				},
			},
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
		if !preview {
			_, err := EC2Client().DeleteVpcPeeringConnection(ctx, &ec2.DeleteVpcPeeringConnectionInput{
				VpcPeeringConnectionId: peeringConnection.VpcPeeringConnectionId,
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"deleted:", aws.ToString(peeringConnection.VpcPeeringConnectionId))
	}
	// delete nacls
	token = nil
	var networkAcls []ec2types.NetworkAcl
	for {
		out, err := EC2Client().DescribeNetworkAcls(ctx, &ec2.DescribeNetworkAclsInput{
			Filters: []ec2types.Filter{
				{
					Name:   aws.String("vpc-id"),
					Values: []string{vpcID},
				},
			},
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
		if networkAcl.IsDefault != nil && *networkAcl.IsDefault {
			continue // cannot delete default nacl
		}
		if !preview {
			_, err := EC2Client().DeleteNetworkAcl(ctx, &ec2.DeleteNetworkAclInput{
				NetworkAclId: networkAcl.NetworkAclId,
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"deleted:", aws.ToString(networkAcl.NetworkAclId))
	}
	// delete subnets
	subnets, err := VpcSubnets(ctx, vpcID)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	for _, subnet := range subnets {
		if !preview {
			_, err := EC2Client().DeleteSubnet(ctx, &ec2.DeleteSubnetInput{
				SubnetId: subnet.SubnetId,
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"deleted:", aws.ToString(subnet.SubnetId))
	}
	// delete vpc
	if !preview {
		_, err = EC2Client().DeleteVpc(ctx, &ec2.DeleteVpcInput{
			VpcId: aws.String(vpcID),
		})
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
	}
	Logger.Println(PreviewString(preview)+"deleted:", name, vpcID)
	return nil
}
