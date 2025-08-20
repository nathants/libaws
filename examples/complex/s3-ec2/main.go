package main

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/nathants/libaws/lib"
)

var (
	uid         = os.Getenv("uid")
	keypairName = "test-keypair-" + uid
	profileName = "test-profile-" + uid
	ec2Name     = "test-ec2-" + uid
	vpcName     = "test-vpc-" + uid
	sgName      = "test-sg-" + uid
	inBucket    = "in-bucket-" + uid
	outBucket   = "out-bucket-" + uid
)

const initTemplate = `#!/bin/bash
(
    set -xeou pipefail
    sudo apt-get update -y
    sudo apt-get install -y awscli
    echo "$(aws s3 cp s3://%s/%s -) from ec2" | aws s3 cp - s3://%s/%s
    sudo poweroff
) &> /tmp/log.txt
`

func formatInitTemplate(inBucket, inKey, outBucket, outKey string) string {
	return fmt.Sprintf(initTemplate, inBucket, inKey, outBucket, outKey)
}

func handleRequest(ctx context.Context, event events.S3Event) (events.APIGatewayProxyResponse, error) {
	sgID, err := lib.EC2SgID(ctx, vpcName, sgName)
	if err != nil {
		panic(err)
	}
	amiID, user, err := lib.EC2AmiBase(ctx, lib.EC2AmiDebianTrixie, lib.EC2ArchAmd64)
	if err != nil {
		panic(err)
	}
	vpcID, err := lib.VpcID(ctx, vpcName)
	if err != nil {
		panic(err)
	}
	zones, err := lib.EC2ZonesWithInstance(ctx, ec2types.InstanceTypeT3Small)
	if err != nil {
		panic(err)
	}
	subnets, err := lib.VpcSubnets(ctx, vpcID)
	if err != nil {
		panic(err)
	}
	var spotSubnetIDs []string
	for _, subnet := range subnets {
		for _, zone := range zones {
			if zone == *subnet.AvailabilityZone {
				spotSubnetIDs = append(spotSubnetIDs, *subnet.SubnetId)
				break
			}
		}
	}
	if len(spotSubnetIDs) == 0 {
		panic("no availability")
	}
	for _, record := range event.Records {
		key := record.S3.Object.Key
		_, err := lib.EC2RequestSpotFleet(ctx, ec2types.AllocationStrategyLowestPrice, &lib.EC2Config{
			NumInstances:   1,
			SgID:           sgID,
			SubnetIds:      spotSubnetIDs,
			Name:           ec2Name + ":" + key,
			AmiID:          amiID,
			Key:            keypairName,
			Profile:        profileName,
			UserName:       user,
			InstanceType:   ec2types.InstanceTypeT3Small,
			SecondsTimeout: 900,
			Gigs:           8,
			Init:           formatInitTemplate(inBucket, key, outBucket, key),
		})
		if err != nil {
			panic(err)
		}
	}
	return events.APIGatewayProxyResponse{StatusCode: 200}, nil
}

func main() {
	lambda.Start(handleRequest)
}
