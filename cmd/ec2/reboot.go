package libaws

import (
	"context"
	"os"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ec2-reboot"] = ec2Reboot
	lib.Args["ec2-reboot"] = ec2RebootArgs{}
}

type ec2RebootArgs struct {
	Selectors []string `arg:"positional,required" help:"instance-id | dns-name | private-dns-name | tag | vpc-id | subnet-id | security-group-id | ip-address | private-ip-address"`
	Preview   bool     `arg:"-p,--preview"`
	Wait      bool     `arg:"-w,--wait" default:"false" help:"wait for ssh"`
}

func (ec2RebootArgs) Description() string {
	return "\nreboot an instance\n"
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
	var ids []string
	for _, instance := range instances {
		ids = append(ids, *instance.InstanceId)
		if instance.State.Name == ec2types.InstanceStateNameRunning || instance.State.Name == ec2types.InstanceStateNameStopped {
			lib.Logger.Println(lib.PreviewString(args.Preview)+"rebooting:", lib.EC2Name(instance.Tags), *instance.InstanceId)
		}
	}
	if args.Preview {
		os.Exit(0)
	}
	if len(ids) == 0 {
		lib.Logger.Fatal("error: no instances found matching selectors")
	}
	_, err = lib.EC2Client().RebootInstances(ctx, &ec2.RebootInstancesInput{
		InstanceIds: ids,
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	if args.Wait {
		_, err := lib.EC2WaitSsh(ctx, &lib.EC2WaitSshInput{
			Selectors:      ids,
			MaxWaitSeconds: 300,
			User:           lib.EC2GetTag(instances[0].Tags, "user", ""),
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
	}
}
