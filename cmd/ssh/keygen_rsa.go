package cliaws

import (
	"io/ioutil"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ssh-keygen-rsa"] = sshKeygenRsa
	lib.Args["ssh-keygen-rsa"] = sshKeygenRsaArgs{}
}

type sshKeygenRsaArgs struct {
}

func (sshKeygenRsaArgs) Description() string {
	return `

`
}

func sshKeygenRsa() {
	var args sshKeygenRsaArgs
	arg.MustParse(&args)
	pubKey, privKey, err := lib.SshKeygenRsa()
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	err = ioutil.WriteFile("id_rsa.pub", []byte(pubKey), os.ModePerm)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	err = ioutil.WriteFile("id_rsa", []byte(privKey), os.ModePerm)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
