package libaws

import (
	"context"
	"fmt"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ec2-describe-sg"] = ec2DescribeSg
	lib.Args["ec2-describe-sg"] = ec2DescribeSgArgs{}
}

type ec2DescribeSgArgs struct {
	Vpc string `arg:"positional,required"`
	Sg  string `arg:"positional,required"`
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
	sgID, err := lib.EC2SgID(ctx, args.Vpc, args.Sg)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	out, err := lib.EC2Client().DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		GroupIds: []string{sgID},
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Fprintln(os.Stderr, "protocol port source")
	for _, perm := range out.SecurityGroups[0].IpPermissions {
		if perm.FromPort == nil {
			perm.FromPort = aws.Int32(0)
		}
		if perm.ToPort == nil {
			perm.ToPort = aws.Int32(65535)
		}
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
