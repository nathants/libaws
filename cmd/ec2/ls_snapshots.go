package cliaws

import (
	"context"
	"fmt"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ec2-ls-snapshots"] = ec2LsSnapshots
lib.Args["ec2-ls-snapshots"] = ec2LsSnapshotsArgs{}
}

type ec2LsSnapshotsArgs struct {
}

func (ec2LsSnapshotsArgs) Description() string {
	return "\nlist snapshots\n"
}

func ec2LsSnapshots() {
	var args ec2LsSnapshotsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	account, err := lib.StsAccount(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	var nextToken *string
	var snapshots []*ec2.Snapshot
	for {
		out, err := lib.EC2Client().DescribeSnapshotsWithContext(ctx, &ec2.DescribeSnapshotsInput{
			OwnerIds:  []*string{aws.String(account)},
			NextToken: nextToken,
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		snapshots = append(snapshots, out.Snapshots...)
		if out.NextToken == nil {
			break
		}
		nextToken = out.NextToken
	}
	for _, snapshot := range snapshots {
		amiID := "-"
		if snapshot.Description != nil {
			for _, part := range strings.Split(*snapshot.Description, " ") {
				if strings.HasPrefix(part, "ami-") {
					amiID = part
					break
				}
			}
		}
		fmt.Println(*snapshot.SnapshotId, amiID, lib.EC2Tags(snapshot.Tags))
	}
}
