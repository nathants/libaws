package libaws

import (
	"context"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/alexflint/go-arg"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ec2-wait-ssh"] = ec2WaitSsh
	lib.Args["ec2-wait-ssh"] = ec2WaitSshArgs{}
}

type ec2WaitSshArgs struct {
	Selectors      []string `arg:"positional,required" help:"instance-id | dns-name | private-dns-name | tag | vpc-id | subnet-id | security-group-id | ip-address | private-ip-address"`
	User           string   `arg:"-u,--user" help:"ssh user if not tagged on instance as 'user'"`
	PrivateIP      bool     `arg:"-p,--private-ip" help:"use ec2 private-ip instead of public-dns for host address"`
	MaxConcurrency int      `arg:"-m,--max-concurrency" default:"32" help:"max concurrent waitssh connections"`
	Key            string   `arg:"-k,--key" help:"waitssh private key"`
	MaxWait        int      `arg:"-w,--max-wait" help:"after this many seconds, terminate any instances not ready and return instance-id of all ready instances"`
	Preview        bool     `arg:"-p,--preview"`
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
	start := time.Now()
	var instances []ec2types.Instance
	var err error
	for {
		instances, err = lib.EC2ListInstances(ctx, args.Selectors, "")
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		if time.Since(start) > 15*time.Second {
			lib.Logger.Fatal("error: no instances found for those selectors")
		}
		if len(instances) > 0 {
			break
		}
		time.Sleep(1 * time.Second)
	}
	for _, instance := range instances {
		states := []ec2types.InstanceStateName{
			ec2types.InstanceStateNamePending,
			ec2types.InstanceStateNameRunning,
		}
		if slices.Contains(states, instance.State.Name) {
			lib.Logger.Println(lib.PreviewString(args.Preview)+"targeting:", lib.EC2Name(instance.Tags), *instance.InstanceId)
		}
	}
	if args.Preview {
		os.Exit(0)
	}
	readyIDs, err := lib.EC2WaitSsh(ctx, &lib.EC2WaitSshInput{
		Selectors:      args.Selectors,
		MaxWaitSeconds: args.MaxWait,
		PrivateIP:      args.PrivateIP,
		User:           args.User,
		Key:            args.Key,
		MaxConcurrency: args.MaxConcurrency,
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, id := range readyIDs {
		fmt.Println(id)
	}
}
