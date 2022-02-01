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
	lib.Commands["ec2-describe-sg"] = ec2DescribeSg
	lib.Args["ec2-describe-sg"] = ec2DescribeSgArgs{}
}

type ec2DescribeSgArgs struct {
	Sg string `arg:"positional,required"`
}

func (ec2DescribeSgArgs) Description() string {
	return "\ndescribe a sg\n"
}

func printSg(proto string, fromPort, toPort int, src string) {
	if proto == "-1" {
		proto = "All"
	}
	var port string
	if fromPort != toPort {
		port = fmt.Sprintf("%d-%d", fromPort, toPort)
	} else {
		port = fmt.Sprint(fromPort)
	}
	fmt.Println(proto, port, src)
}

func ec2DescribeSg() {
	var args ec2DescribeSgArgs
	arg.MustParse(&args)
	ctx := context.Background()
	if !strings.HasPrefix(args.Sg, "sg-") {
		var err error
		args.Sg, err = lib.EC2SgID(ctx, args.Sg)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
	}
	out, err := lib.EC2Client().DescribeSecurityGroupsWithContext(ctx, &ec2.DescribeSecurityGroupsInput{
		GroupIds: []*string{aws.String(args.Sg)},
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	lib.Logger.Println("protocol port source description")
	for _, perm := range out.SecurityGroups[0].IpPermissions {
		for _, ip := range perm.IpRanges {
			printSg(*perm.IpProtocol, int(*perm.FromPort), int(*perm.ToPort), *ip.CidrIp)
		}
		for _, ip := range perm.Ipv6Ranges {
			printSg(*perm.IpProtocol, int(*perm.FromPort), int(*perm.ToPort), *ip.CidrIpv6)
		}
		for _, ip := range perm.UserIdGroupPairs {
			printSg(*perm.IpProtocol, int(*perm.FromPort), int(*perm.ToPort), *ip.GroupId)
		}
		for _, ip := range perm.PrefixListIds {
			printSg(*perm.IpProtocol, int(*perm.FromPort), int(*perm.ToPort), *ip.PrefixListId)
		}
	}
}
