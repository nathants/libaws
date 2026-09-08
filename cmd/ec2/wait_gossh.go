package libaws

import (
	"context"
	"fmt"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ec2-wait-gossh"] = ec2WaitGoSsh
	lib.Args["ec2-wait-gossh"] = ec2WaitGoSshArgs{}
}

type ec2WaitGoSshArgs struct {
	Selectors          []string `arg:"positional,required" help:"instance-id | dns-name | private-dns-name | tag | vpc-id | subnet-id | security-group-id | ip-address | private-ip-address"`
	User               string   `arg:"-u,--user,required" help:"SSH user"`
	MaxConcurrency     int      `arg:"-m,--max-concurrency" default:"32" help:"max concurrent waitgossh connections"`
	MaxWait            int      `arg:"-w,--max-wait" help:"after this many seconds and no instances remain pending, terminate only selected SSH failures and return ready instance IDs; 0 waits indefinitely"`
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
	var rsaBytes, edBytes []byte
	var err error
	if args.RsaPrivKeyFile != "" {
		rsaBytes, err = os.ReadFile(args.RsaPrivKeyFile)
		if err != nil {
			lib.Logger.Fatal("error: read RSA private key: ", err)
		}
	}
	if args.Ed25519PrivKeyFile != "" {
		edBytes, err = os.ReadFile(args.Ed25519PrivKeyFile)
		if err != nil {
			lib.Logger.Fatal("error: read Ed25519 private key: ", err)
		}
	}
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
