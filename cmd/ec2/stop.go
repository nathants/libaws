package cliaws

import (
	"context"
	"os"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ec2-stop"] = ec2Stop
	lib.Args["ec2-stop"] = ec2StopArgs{}
}

type ec2StopArgs struct {
	Selectors []string `arg:"positional,required" help:"instance-id | dns-name | private-dns-name | tag | vpc-id | subnet-id | security-group-id | ip-address | private-ip-address"`
	Preview   bool     `arg:"-p,--preview" default:"false"`
	Wait      bool     `arg:"-w,--wait" default:"false"`
}

func (ec2StopArgs) Description() string {
	return "\ndelete an ami\n"
}

func ec2Stop() {
	var args ec2StopArgs
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
	var ids []*string
	for _, instance := range instances {
		if *instance.State.Name == ec2.InstanceStateNameRunning {
			ids = append(ids, instance.InstanceId)
			lib.Logger.Println("going to stop:", lib.EC2Name(instance.Tags), *instance.InstanceId)
		}
	}
	if args.Preview {
		os.Exit(0)
	}
	_, err = lib.EC2Client().StopInstancesWithContext(ctx, &ec2.StopInstancesInput{
		InstanceIds: ids,
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	if args.Wait {
		for {
			pass := true
			instances, err := lib.EC2ListInstances(ctx, lib.StringSlice(ids), "")
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}
			for _, instance := range instances {
				if ec2.InstanceStateNameStopped != *instance.State.Name {
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
