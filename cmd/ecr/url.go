package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["ecr-url"] = ecrUrl
	lib.Args["ecr-url"] = ecrUrlArgs{}
}

type ecrUrlArgs struct {
}

func (ecrUrlArgs) Description() string {
	return "\nlist ecr containers\n"
}

func ecrUrl() {
	var args ecrUrlArgs
	arg.MustParse(&args)
	ctx := context.Background()
	url, err := lib.EcrUrl(ctx)
	if err != nil {
	    lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(url)
}
