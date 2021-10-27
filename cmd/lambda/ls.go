package cliaws

import (
	"context"
	"fmt"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["lambda-ls"] = lambdaLs
	lib.Args["lambda-ls"] = lambdaLsArgs{}
}

type lambdaLsArgs struct {
}

func (lambdaLsArgs) Description() string {
	return "\nget lambda ls\n"
}

func lambdaLs() {
	var args lambdaLsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	fns, err := lib.LambdaListFunctions(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, fn := range fns {
		name := "-"
		if fn.FunctionName != nil {
			name = *fn.FunctionName
		}
		runtime := "-"
		if fn.Runtime != nil {
			runtime = *fn.Runtime
		}
		timeout := "-"
		if fn.Timeout != nil {
			timeout = fmt.Sprintf("%ds", *fn.Timeout)
		}
		memory := "-"
		if fn.MemorySize != nil {
			memory = fmt.Sprintf("%dmb", *fn.MemorySize)
		}
		lastmodified := "-"
		if fn.LastModified != nil {
			lastmodified = strings.Join(strings.Split(*fn.LastModified, ":")[:2], ":") + "Z"
		}
		fmt.Println(name, runtime, timeout, memory, lastmodified)
	}
}
