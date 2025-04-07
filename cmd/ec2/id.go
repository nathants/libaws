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
	lib.Commands["ec2-id"] = ec2Id
	lib.Args["ec2-id"] = ec2IdArgs{}
}

type ec2IdArgs struct {
	Selectors []string `arg:"positional" help:"instance-id | dns-name | private-dns-name | tag | vpc-id | subnet-id | security-group-id | ip-address | private-ip-address"`
	State     string   `arg:"-s,--state" help:"running | pending | terminated | stopped" default:"running"`
}

func (ec2IdArgs) Description() string {
	return "\nlist ec2 id\n"
}

func ec2Id() {
	var args ec2IdArgs
	arg.MustParse(&args)
	ctx := context.Background()
	instances, err := lib.EC2ListInstances(ctx, args.Selectors, ec2types.InstanceStateName(args.State))
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	if len(instances) == 0 {
		os.Exit(1)
	}
	for _, instance := range instances {
		if instance.InstanceId != nil {
			fmt.Println(*instance.InstanceId)
		}
	}
}
