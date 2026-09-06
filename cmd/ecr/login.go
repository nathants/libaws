package libaws

import (
	"context"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["ecr-login"] = ecrLogin
	lib.Args["ecr-login"] = ecrLoginArgs{}
}

type ecrLoginArgs struct {
}

func (ecrLoginArgs) Description() string {
	return "\nlogin to docker\n"
}

func ecrLogin() {
	var args ecrLoginArgs
	arg.MustParse(&args)
	if err := lib.EcrLogin(context.Background(), os.Stdout, os.Stderr); err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
