package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["acm-ls"] = acmLs
	lib.Args["acm-ls"] = acmLsArgs{}
}

type acmLsArgs struct {
}

func (acmLsArgs) Description() string {
	return "\nlist acm certificates\n"
}

func acmLs() {
	var args acmLsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	certs, err := lib.AcmListCertificates(ctx)
	if err != nil {
	    lib.Logger.Fatal("error: ", err)
	}
	lib.Logger.Println(certs)
}
