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
	Selectors      []string `arg:"positional,required" help:"instance-id | dns-name | private-dns-name | tag | vpc-id | subnet-id | security-group-id | ip-address | private-ip-address"`
	User           string   `arg:"-u,--user" help:"ssh user if not tagged on instance as 'user'"`
	Cmd            string   `arg:"-c,--cmd"`
	Stdin          string   `arg:"-s,--stdin" help:"stdin value to be provided to remote cmd"`
	Timeout        int      `arg:"-t,--timeout" help:"seconds before ssh cmd is considered failed"`
	PrivateIP      bool     `arg:"-p,--private-ip" help:"use ec2 private-ip instead of public-dns for host address"`
	MaxConcurrency int      `arg:"-m,--max-concurrency" default:"32" help:"max concurrent ssh connections"`
	Key            string   `arg:"-k,--key" help:"ssh private key"`
	Yes            bool     `arg:"-y,--yes" default:"false"`
	NoPrint        bool     `arg:"--no-print" default:"false" help:"do not print live output to stdout/stderr"`
}

func (ec2SshArgs) Description() string {
	return "\nssh to ec2 instances\n"
}

func ec2Ssh() {
	var args ec2SshArgs
	arg.MustParse(&args)
	ctx := context.Background()
	fail := true
	for _, s := range args.Selectors {
		if s != "" {
			fail = false
			break
		}
	}
	if fail {
		lib.Logger.Fatal("error: provide some selectors")
	}
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
	if args.Cmd != "" && lib.Exists(args.Cmd) {
		bytes, err := ioutil.ReadFile(args.Cmd)
		if err != nil {
			lib.Logger.Fatal("error:", err)
		}
		args.Cmd = string(bytes)
	}
	stdin := args.Stdin
	if args.Stdin == "-" {
		bytes, err2 := ioutil.ReadAll(os.Stdin)
		if err2 != nil {
			lib.Logger.Fatal("error:", err2)
		}
		stdin = string(bytes)
	}
	if len(instances) == 0 {
		err = fmt.Errorf("no instances found for those selectors")
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
	} else if len(instances) == 1 && args.Cmd == "" {
		err = lib.EC2SshLogin(instances[0], args.User)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
	} else {
		results, err := lib.EC2Ssh(context.Background(), &lib.EC2SshInput{
			User:           args.User,
			TimeoutSeconds: args.Timeout,
			Instances:      instances,
			Cmd:            args.Cmd,
			Stdin:          stdin,
			PrivateIP:      args.PrivateIP,
			MaxConcurrency: args.MaxConcurrency,
			Key:            args.Key,
			PrintLock:      sync.RWMutex{},
		})
		fmt.Fprint(os.Stderr, "\n")
		for _, result := range results {
			if result.Err == nil {
				fmt.Fprintf(os.Stderr, "success: %s\n", lib.Green(result.InstanceID))
			} else {
				fmt.Fprintf(os.Stderr, "failure: %s\n", lib.Red(result.InstanceID))
			}
		}
		if err != nil {
			os.Exit(1)
		}
	}
}
