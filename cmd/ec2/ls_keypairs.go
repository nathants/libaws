package cliaws

import (
	"context"
	"fmt"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ec2-ls-keypairs"] = ec2LsKeypairs
	lib.Args["ec2-ls-keypairs"] = ec2LsKeypairsArgs{}
}

type ec2LsKeypairsArgs struct {
}

func (ec2LsKeypairsArgs) Description() string {
	return "\nlist keypairs\n"
}

func ec2LsKeypairs() {
	var args ec2LsKeypairsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	out, err := lib.EC2Client().DescribeKeyPairsWithContext(ctx, &ec2.DescribeKeyPairsInput{})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Fprintln(os.Stderr, "name", "id", "fingerprint", "type")
	for _, key := range out.KeyPairs {
		fmt.Println(*key.KeyName, *key.KeyPairId, *key.KeyFingerprint, *key.KeyType)
	}
}
