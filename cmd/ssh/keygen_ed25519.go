package cliaws

import (
	"io/ioutil"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ssh-keygen-ed25519"] = sshKeygenEd25519
	lib.Args["ssh-keygen-ed25519"] = sshKeygenEd25519Args{}
}

type sshKeygenEd25519Args struct {
}

func (sshKeygenEd25519Args) Description() string {
	return `

`
}

func sshKeygenEd25519() {
	var args sshKeygenEd25519Args
	arg.MustParse(&args)
	pubKey, privKey, err := lib.SshKeygenEd25519()
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	err = ioutil.WriteFile("id_ed25519.pub", []byte(pubKey), os.ModePerm)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	err = ioutil.WriteFile("id_ed25519", []byte(privKey), os.ModePerm)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
