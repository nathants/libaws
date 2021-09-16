package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["iam-ls-ssh-keys"] = iamLsSSHKeys
lib.Args["iam-ls-ssh-keys"] = iamLsSSHKeysArgs{}
}

type iamLsSSHKeysArgs struct {
}

func (iamLsSSHKeysArgs) Description() string {
	return "\nlist iam ssh keys for the caller's iam user\n"
}

func iamLsSSHKeys() {
	var args iamLsSSHKeysArgs
	arg.MustParse(&args)
	ctx := context.Background()
	keys, err := lib.IamListSSHPublicKeys(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, key := range keys {
		data, err := lib.IamGetSSHPublicKey(ctx, *key.SSHPublicKeyId)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		fmt.Println(*key.Status, *key.SSHPublicKeyId, *data.Fingerprint)
	}
}
