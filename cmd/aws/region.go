package cliaws

import (
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["aws-region"] = region
	lib.Args["aws-region"] = regionArgs{}
}

type regionArgs struct {
}

func (regionArgs) Description() string {
	return "\ncurrent region id\n"
}

func region() {
	var args regionArgs
	arg.MustParse(&args)
	fmt.Println(lib.Region())
}
