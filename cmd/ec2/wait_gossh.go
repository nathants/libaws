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
	lib.Commands["ec2-wait-gossh"] = ec2WaitGoSsh
	lib.Args["ec2-wait-gossh"] = ec2WaitGoSshArgs{}
}

type ec2WaitGoSshArgs struct {
	Selectors          []string `arg:"positional,required" help:"instance-id | dns-name | private-dns-name | tag | vpc-id | subnet-id | security-group-id | ip-address | private-ip-address"`
	User               string   `arg:"-u,--user" help:"gossh user if not tagged on instance as 'user'"`
	MaxConcurrency     int      `arg:"-m,--max-concurrency" default:"32" help:"max concurrent waitgossh connections"`
	MaxWait            int      `arg:"-w,--max-wait" help:"after this many seconds, terminate any instances not ready and return instance-id of all ready instances"`
	Ed25519PrivKeyFile string   `arg:"-e,--ed25519" help:"private key"`
	RsaPrivKeyFile     string   `arg:"-r,--rsa" help:"private key"`
}

func (ec2WaitGoSshArgs) Description() string {
	return "\nwait for gossh to succeed then return their instance ids\n"
}

func ec2WaitGoSsh() {
	var args ec2WaitGoSshArgs
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
			err = fmt.Errorf("no instances found for those selectors")
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}
		}
		if len(instances) > 0 {
			break
		}
	}
	for _, instance := range instances {
		states := []ec2types.InstanceStateName{
			ec2types.InstanceStateNamePending,
			ec2types.InstanceStateNameRunning,
		}
		if slices.Contains(states, instance.State.Name) {
			lib.Logger.Println(lib.EC2Name(instance.Tags), *instance.InstanceId)
		}
	}
	rsaBytes, _ := os.ReadFile(args.RsaPrivKeyFile)
	edBytes, _ := os.ReadFile(args.Ed25519PrivKeyFile)
	readyIDs, err := lib.EC2WaitGoSsh(ctx, &lib.EC2WaitGoSshInput{
		Selectors:      args.Selectors,
		MaxWaitSeconds: args.MaxWait,
		User:           args.User,
		MaxConcurrency: args.MaxConcurrency,
		RsaPrivKey:     string(rsaBytes),
		Ed25519PrivKey: string(edBytes),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, id := range readyIDs {
		fmt.Println(id)
	}
}
