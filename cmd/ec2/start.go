package libaws

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ec2-start"] = ec2Start
	lib.Args["ec2-start"] = ec2StartArgs{}
}

type ec2StartArgs struct {
	Selectors []string `arg:"positional,required" help:"instance-id | dns-name | private-dns-name | tag | vpc-id | subnet-id | security-group-id | ip-address | private-ip-address"`
	Preview   bool     `arg:"-p,--preview" default:"false"`
	Wait      bool     `arg:"-w,--wait" default:"false"`
}

func (ec2StartArgs) Description() string {
	return "\nstart an instance\n"
}

func ec2Start() {
	var args ec2StartArgs
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
	fmt.Println(args.Selectors)
	instances, err := lib.EC2ListInstances(ctx, args.Selectors, "")
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	var ids []string
	for _, instance := range instances {
		if instance.State.Name == ec2types.InstanceStateNameStopped {
			ids = append(ids, *instance.InstanceId)
			lib.Logger.Println(lib.PreviewString(args.Preview)+"startping:", lib.EC2Name(instance.Tags), *instance.InstanceId)
		}
	}
	if args.Preview {
		os.Exit(0)
	}
	_, err = lib.EC2Client().StartInstances(ctx, &ec2.StartInstancesInput{
		InstanceIds: ids,
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	if args.Wait {
		for {
			pass := true
			instances, err := lib.EC2ListInstances(ctx, ids, "")
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}
			for _, instance := range instances {
				if ec2types.InstanceStateNameRunning != instance.State.Name {
					pass = false
				}
			}
			if pass {
				break
			}
			time.Sleep(1 * time.Second)
		}
	}
}
