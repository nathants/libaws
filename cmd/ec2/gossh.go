package libaws

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/alexflint/go-arg"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ec2-gossh"] = ec2Gossh
	lib.Args["ec2-gossh"] = ec2GosshArgs{}
}

type ec2GosshArgs struct {
	Selectors          []string `arg:"positional,required" help:"instance-id | dns-name | private-dns-name | tag | vpc-id | subnet-id | security-group-id | ip-address | private-ip-address"`
	User               string   `arg:"-u,--user" help:"gossh user if not tagged on instance as 'user'"`
	Cmd                string   `arg:"-c,--cmd"`
	Stdin              string   `arg:"-s,--stdin" help:"stdin value to be provided to remote cmd"`
	Timeout            int      `arg:"-t,--timeout" help:"seconds before gossh cmd is considered failed"`
	MaxConcurrency     int      `arg:"-m,--max-concurrency" default:"32" help:"max concurrent gossh connections"`
	Ed25519PrivKeyFile string   `arg:"-e,--ed25519" help:"private key"`
	RsaPrivKeyFile     string   `arg:"-r,--rsa" help:"private key"`
}

func (ec2GosshArgs) Description() string {
	return "\ngossh to ec2 instances\n"
}

func ec2Gossh() {
	var args ec2GosshArgs
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
		lib.Logger.Println("targeting:", lib.EC2Name(instance.Tags), *instance.InstanceId)
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
	} else {
		rsaBytes, _ := os.ReadFile(args.RsaPrivKeyFile)
		rsaPrivKey := string(rsaBytes)
		edBytes, _ := os.ReadFile(args.Ed25519PrivKeyFile)
		ed25519PrivKey := string(edBytes)
		var targetAddrs []string
		for _, instance := range instances {
			targetAddrs = append(targetAddrs, *instance.PublicDnsName)
		}
		_, err := lib.EC2GoSsh(context.Background(), &lib.EC2GoSshInput{
			NoTTY:          true,
			User:           args.User,
			TimeoutSeconds: args.Timeout,
			TargetAddrs:    targetAddrs,
			Cmd:            args.Cmd,
			Stdin:          stdin,
			MaxConcurrency: args.MaxConcurrency,
			RsaPrivKey:     rsaPrivKey,
			Ed25519PrivKey: ed25519PrivKey,
			Stdout:         os.Stdout,
			Stderr:         os.Stderr,
		})
		if err != nil {
			os.Exit(1)
		}
	}
}
