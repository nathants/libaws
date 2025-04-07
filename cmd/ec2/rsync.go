package libaws

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/alexflint/go-arg"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ec2-rsync"] = ec2Rsync
	lib.Args["ec2-rsync"] = ec2RsyncArgs{}
}

type ec2RsyncArgs struct {
	Source         string   `arg:"positional,required"`
	Destination    string   `arg:"positional,required"`
	Selectors      []string `arg:"positional,required" help:"instance-id | dns-name | private-dns-name | tag | vpc-id | subnet-id | security-group-id | ip-address | private-ip-address"`
	User           string   `arg:"-u,--user" help:"ssh user if not tagged on instance as 'user'"`
	PrivateIP      bool     `arg:"-p,--private-ip" help:"use ec2 private-ip instead of public-dns for host address"`
	MaxConcurrency int      `arg:"-m,--max-concurrency" default:"32" help:"max concurrent rsync connections"`
	Key            string   `arg:"-k,--key" help:"rsync private key"`
	Preview        bool     `arg:"-p,--preview"`
}

func (ec2RsyncArgs) Description() string {
	return "\nrsync to ec2 instances\n"
}

func ec2Rsync() {
	var args ec2RsyncArgs
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
	if len(instances) == 0 {
		err = fmt.Errorf("no instances found for those selectors")
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
	}
	results, err := lib.EC2Rsync(context.Background(), &lib.EC2RsyncInput{
		Source:         args.Source,
		Destination:    args.Destination,
		User:           args.User,
		Instances:      instances,
		PrivateIP:      args.PrivateIP,
		MaxConcurrency: args.MaxConcurrency,
		Key:            args.Key,
		PrintLock:      sync.Mutex{},
	})
	var lastErr error
	for _, result := range results {
		if result.Err == nil {
			fmt.Fprintf(os.Stderr, "success: %s\n", lib.Green(result.InstanceID))
		} else {
			lastErr = err
			fmt.Fprintf(os.Stderr, "failure: %s\n", lib.Red(result.InstanceID))
		}
	}
	if lastErr != nil {
		os.Exit(1)
	}
}
