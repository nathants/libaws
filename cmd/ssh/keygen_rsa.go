package cliaws

import (
	"io/ioutil"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ssh-keygen-rsa"] = sshKeygenRsa
	lib.Args["ssh-keygen-rsa"] = sshKeygenRsaArgs{}
}

type sshKeygenRsaArgs struct {
}

func (sshKeygenRsaArgs) Description() string {
	return "\ngenerate new rsa ssh keypair and write id_rsa and id_rsa.pub to current working directory\n"
}

func sshKeygenRsa() {
	var args sshKeygenRsaArgs
	arg.MustParse(&args)
	pubKey, privKey, err := lib.SshKeygenRsa()
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	err = ioutil.WriteFile("id_rsa.pub", []byte(pubKey), 0600)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	err = ioutil.WriteFile("id_rsa", []byte(privKey), 0600)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
