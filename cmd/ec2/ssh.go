package cliaws

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ec2-ssh"] = ec2Ssh
}

type ec2SshArgs struct {
	Selectors []string `arg:"positional" help:"instance-ids | dns-names | private-dns-names | tags | vpc-id | subnet-id | security-group-id | ip-addresses | private-ip-addresses"`
	User      string   `arg:"-u,--user" help:"ssh user if not tagged on instance as 'user'"`
	Cmd       string   `arg:"-c,--cmd"`
	Stdin     string   `arg:"-s,--stdin" help:"stdin value to be provided to remote cmd"`
	NoTTY     bool     `arg:"--no-tty" help:"when backgrounding a process, you dont want a tty"`
	Timeout   int      `arg:"-t,--timeout" help:"seconds before ssh cmd is considered failed"`
}

func (ec2SshArgs) Description() string {
	return "\nssh to ec2 instances\n"
}

func ec2Ssh() {
	var args ec2SshArgs
	arg.MustParse(&args)
	ctx := context.Background()
	instances, err := lib.EC2ListInstances(ctx, args.Selectors, ec2.InstanceStateNameRunning)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, instance := range instances {
		lib.Logger.Println(*instance.InstanceId)
	}
	var stdin string
	if args.Stdin == "-" {
		bytes, err2 := ioutil.ReadAll(os.Stdin)
		if err2 != nil {
			lib.Logger.Fatal("error:", err2)
		}
		stdin = string(bytes)
	}
	if len(instances) == 0 {
		err = fmt.Errorf("no instances found for those selectors")
	} else if len(instances) == 1 && args.Cmd != "" {
		err = lib.EC2SshLogin(instances[0], args.User)
	} else {
		err = lib.EC2Ssh(context.Background(), &lib.EC2SshInput{
			User:           args.User,
			TimeoutSeconds: args.Timeout,
			Instances:      instances,
			Cmd:            args.Cmd,
			Stdout:         os.Stdout,
			Stderr:         os.Stderr,
			Stdin:          stdin,
			NoTTY:          args.NoTTY,
		})
	}
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
