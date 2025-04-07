package libaws

import (
	"os"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ssh-keygen-ed25519"] = sshKeygenEd25519
	lib.Args["ssh-keygen-ed25519"] = sshKeygenEd25519Args{}
}

type sshKeygenEd25519Args struct {
}

func (sshKeygenEd25519Args) Description() string {
	return "\ngenerate new ed25519 ssh keypair and write id_ed25519 and id_ed25519.pub to current working directory\n"
}

func sshKeygenEd25519() {
	var args sshKeygenEd25519Args
	arg.MustParse(&args)
	pubKey, privKey, err := lib.SshKeygenEd25519()
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	err = os.WriteFile("id_ed25519.pub", []byte(pubKey), 0600)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	err = os.WriteFile("id_ed25519", []byte(privKey), 0600)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
