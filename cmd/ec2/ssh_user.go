package libaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ec2-ssh-user"] = ec2SshUser
	lib.Args["ec2-ssh-user"] = ec2SshUserArgs{}
}

type ec2SshUserArgs struct {
	Selectors []string `arg:"positional,required" help:"instance-id | dns-name | private-dns-name | tag | vpc-id | subnet-id | security-group-id | ip-address | private-ip-address"`
}

func (ec2SshUserArgs) Description() string {
	return "\nprint the ssh-user for ec2 instance\n"
}

func ec2SshUser() {
	var args ec2SshUserArgs
	arg.MustParse(&args)
	ctx := context.Background()
	instances, err := lib.EC2ListInstances(ctx, args.Selectors, "running")
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	user, err := lib.EC2SshUser(instances, "")
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(user)
}
