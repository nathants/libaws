package libaws

import (
	"context"
	"fmt"
	"os"

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
	users := map[string]any{}
	for _, instance := range instances {
		users[lib.EC2GetTag(instance.Tags, "user", "")] = nil
	}
	if len(users) > 1 {
		lib.Logger.Fatal("error: too many users ", lib.Pformat(instances))
	}
	for user := range users {
		fmt.Println(user)
		os.Exit(0)
	}
	lib.Logger.Fatal("no user tag found")
}
