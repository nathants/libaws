package cliaws

import (
	"context"
	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ec2-reboot"] = ec2Reboot
	lib.Args["ec2-reboot"] = ec2RebootArgs{}
}

type ec2RebootArgs struct {
	Selectors []string `arg:"positional,required" help:"instance-id | dns-name | private-dns-name | tag | vpc-id | subnet-id | security-group-id | ip-address | private-ip-address"`
	Yes       bool     `arg:"-y,--yes" default:"false"`
}

func (ec2RebootArgs) Description() string {
	return "\ndelete an ami\n"
}

func ec2Reboot() {
	var args ec2RebootArgs
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
		ids = append(ids, instance.InstanceId)
		if *instance.State.Name == ec2.InstanceStateNameRunning || *instance.State.Name == ec2.InstanceStateNameStopped {
			lib.Logger.Println("going to reboot:", lib.EC2Name(instance.Tags), *instance.InstanceId)
		}
	}
	if !args.Yes {
		err = lib.PromptProceed("")
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
	}
	_, err = lib.EC2Client().RebootInstancesWithContext(ctx, &ec2.RebootInstancesInput{
		InstanceIds: ids,
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
