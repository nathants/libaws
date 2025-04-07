package libaws

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/alexflint/go-arg"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ec2-ssh"] = ec2Ssh
	lib.Args["ec2-ssh"] = ec2SshArgs{}
}

type ec2SshArgs struct {
	Selectors      []string `arg:"positional,required" help:"instance-id | dns-name | private-dns-name | tag | vpc-id | subnet-id | security-group-id | ip-address | private-ip-address"`
	User           string   `arg:"-u,--user" help:"ssh user if not tagged on instance as 'user'"`
	Cmd            string   `arg:"-c,--cmd"`
	Stdin          string   `arg:"-s,--stdin" help:"stdin value to be provided to remote cmd.\n                         if stdin is -, all content is read from stdin, and then passed to ssh.\n                         to stream through stdin, use: ssh $(libaws ec2-ssh-user INSTANCE)@$(libaws ec2-ip INSTANCE)"`
	Timeout        int      `arg:"-t,--timeout" help:"seconds before ssh cmd is considered failed"`
	PrivateIP      bool     `arg:"-p,--private-ip" help:"use ec2 private-ip instead of public-dns for host address"`
	MaxConcurrency int      `arg:"-m,--max-concurrency" default:"32" help:"max concurrent ssh connections"`
	Key            string   `arg:"-k,--key" help:"ssh private key"`
	Preview        bool     `arg:"-p,--preview" default:"false"`
	NoPrint        bool     `arg:"--no-print" default:"false" help:"do not print live output to stdout/stderr"`
	IPNotID        bool     `arg:"-i,--ip" default:"false" help:"when targeting multiple instances, prefix output lines with ipv4 not instance-id"`
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
	instances, err := lib.EC2ListInstances(ctx, args.Selectors, ec2types.InstanceStateNameRunning)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, instance := range instances {
		lib.Logger.Println(lib.PreviewString(args.Preview)+"targeting:", lib.EC2Name(instance.Tags), *instance.InstanceId)
	}
	if args.Preview {
		os.Exit(0)
	}
	if args.Cmd != "" && lib.Exists(args.Cmd) {
		bytes, err := os.ReadFile(args.Cmd)
		if err != nil {
			lib.Logger.Fatal("error:", err)
		}
		args.Cmd = string(bytes)
	}
	stdin := args.Stdin
	if args.Stdin == "-" {
		bytes, err2 := io.ReadAll(os.Stdin)
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
		err = lib.EC2SshLogin(instances[0], args.User, args.Key)
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
			PrintLock:      sync.Mutex{},
			IPNotID:        args.IPNotID,
		})
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
