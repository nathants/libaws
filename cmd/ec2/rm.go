package cliaws

import (
	"context"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ec2-rm"] = ec2Rm
	lib.Args["ec2-rm"] = ec2RmArgs{}
}

type ec2RmArgs struct {
	Selectors []string `arg:"positional,required" help:"instance-id | dns-name | private-dns-name | tag | vpc-id | subnet-id | security-group-id | ip-address | private-ip-address"`
	Preview   bool     `arg:"-p,--preview" default:"false"`
}

func (ec2RmArgs) Description() string {
	return "\ndelete an ami\n"
}

func ec2Rm() {
	var args ec2RmArgs
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
		if *instance.State.Name == ec2.InstanceStateNameTerminated {
			continue
		}
		ids = append(ids, instance.InstanceId)
		if *instance.State.Name == ec2.InstanceStateNameRunning || *instance.State.Name == ec2.InstanceStateNameStopped {
			lib.Logger.Println(lib.PreviewString(args.Preview)+"terminating:", lib.EC2Name(instance.Tags), *instance.InstanceId)
		}
	}
	if args.Preview {
		os.Exit(0)
	}
	if len(ids) == 0 {
		os.Exit(0)
	}
	_, err = lib.EC2Client().TerminateInstancesWithContext(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: ids,
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	err = lib.EC2WaitState(ctx, lib.StringSlice(ids), "terminated")
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
