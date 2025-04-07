package libaws

import (
	"context"
	"fmt"
	"os"

	"github.com/alexflint/go-arg"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ec2-dns"] = ec2Dns
	lib.Args["ec2-dns"] = ec2DnsArgs{}
}

type ec2DnsArgs struct {
	Selectors []string `arg:"positional" help:"instance-id | dns-name | private-dns-name | tag | vpc-id | subnet-id | security-group-id | ip-address | private-ip-address"`
}

func (ec2DnsArgs) Description() string {
	return "\nlist ec2 dns\n"
}

func ec2Dns() {
	var args ec2DnsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	instances, err := lib.EC2ListInstances(ctx, args.Selectors, ec2types.InstanceStateNameRunning)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	if len(instances) == 0 {
		os.Exit(1)
	}
	for _, instance := range instances {
		if instance.PrivateDnsName != nil {
			fmt.Println(*instance.PublicDnsName)
		}
	}
}
