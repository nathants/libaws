package cliaws

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os/exec"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/nathants/cli-aws/lib"
	"golang.org/x/crypto/ssh"
)

func init() {
	lib.Commands["ec2-ensure-keypair"] = ec2EnsureKeypair
	lib.Args["ec2-ensure-keypair"] = ec2EnsureKeypairArgs{}
}

type ec2EnsureKeypairArgs struct {
	Name   string `arg:"positional,required"`
	PubKey string `arg:"positional,required" help:"path to pubkey file"`
}

func (ec2EnsureKeypairArgs) Description() string {
	return "\nensure a keypair\n"
}

func ec2EnsureKeypair() {
	var args ec2EnsureKeypairArgs
	arg.MustParse(&args)
	ctx := context.Background()
	data, err := ioutil.ReadFile(args.PubKey)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	_, err = lib.EC2Client().ImportKeyPairWithContext(ctx, &ec2.ImportKeyPairInput{
		KeyName:           aws.String(args.Name),
		PublicKeyMaterial: data,
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != "InvalidKeyPair.Duplicate" {
			lib.Logger.Fatal("error: ", err)
		}
		out, err := lib.EC2Client().DescribeKeyPairsWithContext(ctx, &ec2.DescribeKeyPairsInput{
			KeyNames: []*string{aws.String(args.Name)},
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		pubkey, _, _, _, err := ssh.ParseAuthorizedKey(data)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		if len(out.KeyPairs) != 1 {
			lib.Logger.Fatal("more than 1 key found: %s", lib.Pformat(out))
		}
		switch pubkey.Type() {
		case ssh.KeyAlgoED25519:
			remoteFingerprint := *out.KeyPairs[0].KeyFingerprint
			if remoteFingerprint[len(remoteFingerprint)-1] == '=' {
				remoteFingerprint = remoteFingerprint[:len(remoteFingerprint)-1]
			}
			localFingerprint := strings.SplitN(ssh.FingerprintSHA256(pubkey), ":", 2)[1]
			if remoteFingerprint != localFingerprint {
				lib.Logger.Fatalf("key exists, but pubkeys do not match: remote=%s != local=%s", remoteFingerprint, localFingerprint)
			}
		case ssh.KeyAlgoRSA:
			cmd := exec.Command("bash", "-c", fmt.Sprintf(`ssh-keygen -e -f %s -m pkcs8 | openssl pkey -pubin -outform der | openssl md5 -c | cut -d" " -f2`, args.PubKey))
			var stdout bytes.Buffer
			cmd.Stdout = &stdout
			err := cmd.Run()
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}
			remoteFingerprint := *out.KeyPairs[0].KeyFingerprint
			localFingerprint := strings.TrimRight(stdout.String(), "\n")
			if remoteFingerprint != localFingerprint {
				lib.Logger.Fatalf("key exists, but pubkeys do not match: remote=%s != local=%s", remoteFingerprint, localFingerprint)
			}
		default:
			lib.Logger.Fatal("bad key type: %s", pubkey.Type())
		}
	}
}
