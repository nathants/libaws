package libaws

import (
	"context"
	"fmt"
	"time"

	"github.com/alexflint/go-arg"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ec2-wait-state"] = ec2WaitState
	lib.Args["ec2-wait-state"] = ec2WaitStateArgs{}
}

type ec2WaitStateArgs struct {
	State     string   `arg:"positional,required"`
	Selectors []string `arg:"positional,required" help:"instance-id | dns-name | private-dns-name | tag | vpc-id | subnet-id | security-group-id | ip-address | private-ip-address"`
}

func (ec2WaitStateArgs) Description() string {
	return "\nwait for state\n"
}

func ec2WaitState() {
	var args ec2WaitStateArgs
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
	for {
		var instances []ec2types.Instance
		var err error
		for {
			instances, err = lib.EC2ListInstances(ctx, args.Selectors, "")
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}
			if time.Since(start) > 300*time.Second {
				err = fmt.Errorf("no instances found for those selectors")
				if err != nil {
					lib.Logger.Fatal("error: ", err)
				}
			}
			if len(instances) > 0 {
				break
			}
		}
		pass := true
		for _, instance := range instances {
			if args.State != string(instance.State.Name) {
				fmt.Println(instance.State.Name, *instance.InstanceId)
				pass = false
			}
		}
		if pass {
			break
		}
		time.Sleep(1 * time.Second)
	}
}
