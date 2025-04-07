package libaws

import (
	"context"
	"fmt"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["vpc-ensure-flowlogs"] = vpcEnsureFlowlogs
	lib.Args["vpc-ensure-flowlogs"] = vpcEnsureFlowlogsArgs{}
}

type vpcEnsureFlowlogsArgs struct {
	Name string `arg:"positional,required"`
}

func (vpcEnsureFlowlogsArgs) Description() string {
	return "\nensure vpc flow logs for monitoring ec2 outbound bandwidth\n"
}

const flowLogsPolicyTemplate = `{
  "Version": "2012-10-17",
  "Id": "AWSLogDeliveryWrite20150319",
  "Statement": [
    {
      "Sid": "AWSLogDeliveryWrite",
      "Effect": "Allow",
      "Principal": {
        "Service": "delivery.logs.amazonaws.com"
      },
      "Action": "s3:PutObject",
      "Resource": "arn:aws:s3:::%s/AWSLogs/%s/*",
      "Condition": {
        "StringEquals": {
          "s3:x-amz-acl": "bucket-owner-full-control",
          "aws:SourceAccount": "%s"
        },
        "ArnLike": {
          "aws:SourceArn": "arn:aws:logs:%s:%s:*"
        }
      }
    },
    {
      "Sid": "AWSLogDeliveryAclCheck",
      "Effect": "Allow",
      "Principal": {
        "Service": "delivery.logs.amazonaws.com"
      },
      "Action": "s3:GetBucketAcl",
      "Resource": "arn:aws:s3:::%s",
      "Condition": {
        "StringEquals": {
          "aws:SourceAccount": "%s"
        },
        "ArnLike": {
          "aws:SourceArn": "arn:aws:logs:%s:%s:*"
        }
      }
    }
  ]
}`

func vpcEnsureFlowlogs() {
	var args vpcEnsureFlowlogsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	vpcID, err := lib.VpcID(ctx, args.Name)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	account, err := lib.StsAccount(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	bucketName := fmt.Sprintf("vpc-flowlogs-%s-%s", args.Name, account)
	input, err := lib.S3EnsureInput("", bucketName, []string{"acl=private", "ttldays=7"})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	region := lib.Region()
	customPolicy := fmt.Sprintf(flowLogsPolicyTemplate, bucketName, account, account, account, region, bucketName, account, region, account)
	customPolicy = strings.ReplaceAll(customPolicy, " ", "")
	customPolicy = strings.ReplaceAll(customPolicy, "\n", "")
	input.CustomPolicy = aws.String(customPolicy)
	err = lib.S3Ensure(ctx, input, false)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	out, err := lib.EC2Client().DescribeFlowLogs(ctx, &ec2.DescribeFlowLogsInput{
		Filter: []ec2types.Filter{{
			Name:   aws.String("resource-id"),
			Values: []string{vpcID},
		}},
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	logFormat := "${instance-id} ${start} ${bytes} ${pkt-dstaddr} ${pkt-dst-aws-service}"
	bucketArn := fmt.Sprintf("arn:aws:s3:::%s", bucketName)
	aggregationInterval := int32(60)
	switch len(out.FlowLogs) {
	case 0:
		_, err := lib.EC2Client().CreateFlowLogs(ctx, &ec2.CreateFlowLogsInput{
			LogDestination:         aws.String(bucketArn),
			LogDestinationType:     ec2types.LogDestinationTypeS3,
			MaxAggregationInterval: aws.Int32(aggregationInterval),
			TrafficType:            ec2types.TrafficTypeAll,
			LogFormat:              aws.String(logFormat),
			ResourceType:           ec2types.FlowLogsResourceTypeVpc,
			ResourceIds:            []string{vpcID},
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		lib.Logger.Println("created vpc flow logs")
	case 1:
		flowLogs := out.FlowLogs[0]
		if *flowLogs.LogFormat != logFormat {
			panic("invalid log format")
		}
		if *flowLogs.MaxAggregationInterval != aggregationInterval {
			panic("invalid aggregation interval")
		}
		if *flowLogs.LogDestination != bucketArn {
			panic("invalid log destination")
		}
		if flowLogs.LogDestinationType != ec2types.LogDestinationTypeS3 {
			panic("invalid log destination type")
		}
		if flowLogs.TrafficType != ec2types.TrafficTypeAll {
			panic("invalid traffic type")
		}
		if *flowLogs.ResourceId != vpcID {
			panic("invalid vpc id")
		}
	default:
		panic(lib.PformatAlways(out))
	}
}
