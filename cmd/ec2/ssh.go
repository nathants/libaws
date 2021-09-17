package cliaws

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"sync"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ec2-ssh"] = ec2Ssh
	lib.Args["ec2-ssh"] = ec2SshArgs{}
}

type ec2SshArgs struct {
	Selectors      []string `arg:"positional" help:"instance-ids | dns-names | private-dns-names | tags | vpc-id | subnet-id | security-group-id | ip-addresses | private-ip-addresses"`
	User           string   `arg:"-u,--user" help:"ssh user if not tagged on instance as 'user'"`
	Cmd            string   `arg:"-c,--cmd"`
	Stdin          string   `arg:"-s,--stdin" help:"stdin value to be provided to remote cmd"`
	NoTTY          bool     `arg:"-n,--no-tty" help:"when backgrounding a process, you dont want a tty"`
	Timeout        int      `arg:"-t,--timeout" help:"seconds before ssh cmd is considered failed"`
	PrivateIP      bool     `arg:"-p,--private-ip" help:"use ec2 private-ip instead of public-dns for host address"`
	MaxConcurrency int      `arg:"-m,--max-concurrency" default:"32" help:"max concurrent ssh connections"`
	Key            string   `arg:"-k,--key" help:"ssh private key"`
	Yes            bool     `arg:"-y,--yes" default:"false"`
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
		lib.Logger.Println("going to target:", lib.EC2Name(instance.Tags), *instance.InstanceId)
	}
	if !args.Yes {
		err = lib.PromptProceed("")
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
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
	} else if len(instances) == 1 && args.Cmd == "" {
		err = lib.EC2SshLogin(instances[0], args.User)
	} else {
		_, err = lib.EC2Ssh(context.Background(), &lib.EC2SshInput{
			User:           args.User,
			TimeoutSeconds: args.Timeout,
			Instances:      instances,
			Cmd:            args.Cmd,
			Stdin:          stdin,
			NoTTY:          args.NoTTY,
			PrivateIP:      args.PrivateIP,
			MaxConcurrency: args.MaxConcurrency,
			Key:            args.Key,
			PrintLock:      sync.RWMutex{},
		})
	}
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
