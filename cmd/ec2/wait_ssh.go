package cliaws

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ec2-wait-ssh"] = ec2WaitSsh
	lib.Args["ec2-wait-ssh"] = ec2WaitSshArgs{}
}

type ec2WaitSshArgs struct {
	Selectors      []string `arg:"positional,required" help:"instance-id | dns-name | private-dns-name | tag | vpc-id | subnet-id | security-group-id | ip-address | private-ip-address"`
	User           string   `arg:"-u,--user" help:"ssh user if not tagged on instance as 'user'"`
	Cmd            string   `arg:"-c,--cmd"`
	Stdin          string   `arg:"-s,--stdin" help:"stdin value to be provided to remote cmd"`
	PrivateIP      bool     `arg:"-p,--private-ip" help:"use ec2 private-ip instead of public-dns for host address"`
	MaxConcurrency int      `arg:"-m,--max-concurrency" default:"32" help:"max concurrent waitssh connections"`
	Key            string   `arg:"-k,--key" help:"waitssh private key"`
	MaxWait        int      `arg:"-w,--max-wait" help:"after this many seconds, terminate any instances not ready and return instance-id of all ready instances"`
	Yes            bool     `arg:"-y,--yes" default:"false"`
}

func (ec2WaitSshArgs) Description() string {
	return "\nwait for ssh to succeed then return their instance ids\n"
}

func ec2WaitSsh() {
	var args ec2WaitSshArgs
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
	instances, err := lib.EC2ListInstances(ctx, args.Selectors, "")
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	if len(instances) == 0 {
		err = fmt.Errorf("no instances found for those selectors")
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
	}
	for _, instance := range instances {
		if lib.Contains([]string{ec2.InstanceStateNamePending, ec2.InstanceStateNameRunning}, *instance.State.Name) {
			lib.Logger.Println("going to target:", lib.EC2Name(instance.Tags), *instance.InstanceId)
		}
	}
	if !args.Yes {
		err = lib.PromptProceed("")
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
	}
	start := time.Now()
	for {
		instances, err := lib.EC2ListInstances(ctx, args.Selectors, ec2.InstanceStateNameRunning)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		//
		var ips []string
		for _, instance := range instances {
			if args.PrivateIP {
				ips = append(ips, *instance.PrivateIpAddress)
			} else {
				ips = append(ips, *instance.PublicDnsName)
			}
		}
		// add an executable on PATH named `aws-ec2-ip-callback` which
		// will be invoked with the ipv4 of all instances to be waited
		// before each attempt
		_ = exec.Command("bash", "-c", fmt.Sprintf("aws-ec2-ip-callback %s && sleep 1", strings.Join(ips, " "))).Run()
		//
		results, err := lib.EC2Ssh(context.Background(), &lib.EC2SshInput{
			User:           args.User,
			TimeoutSeconds: 10,
			Instances:      instances,
			Cmd:            "whoami >/dev/null",
			PrivateIP:      args.PrivateIP,
			MaxConcurrency: args.MaxConcurrency,
			Key:            args.Key,
			PrintLock:      sync.RWMutex{},
			NoPrint:        true,
		})
		for _, result := range results {
			if result.Err == nil {
				fmt.Fprintf(os.Stderr, "ready: %s\n", lib.Green(result.InstanceID))
			} else {
				fmt.Fprintf(os.Stderr, "unready: %s\n", lib.Red(result.InstanceID))
			}
		}
		if err == nil {
			for _, result := range results {
				fmt.Println(result.InstanceID)
			}
			return
		}
		if args.MaxWait != 0 && time.Since(start) > time.Duration(args.MaxWait)*time.Second {
			var ready []string
			var terminate []string
			for _, result := range results {
				if result.Err != nil {
					fmt.Fprintln(os.Stderr, "terminating unready instance:", result.InstanceID)
					terminate = append(terminate, result.InstanceID)
				} else {
					ready = append(ready, result.InstanceID)
				}
			}
			_, err = lib.EC2Client().TerminateInstancesWithContext(ctx, &ec2.TerminateInstancesInput{
				InstanceIds: aws.StringSlice(terminate),
			})
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}
			for _, id := range ready {
				fmt.Println(id)
			}
			return
		}
		time.Sleep(5 * time.Second)
	}
}
