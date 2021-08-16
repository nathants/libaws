package cliaws

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["infra-ls"] = infraLs
}

type infraLsArgs struct {
	SplitSpaces bool `arg:"-s,--split-spaces" help:"split strings on space so attributes are one per line"`
}

func (infraLsArgs) Description() string {
	return "\nls infra\n"
}

func infraLs() {
	var args infraLsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	infra, err := lib.InfraList(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	str := lib.Pformat(infra)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	if args.SplitSpaces {
		val := make(map[string][]string)
		newVal := make(map[string][][]string)
		err := json.Unmarshal([]byte(str), &val)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		for infraType, values := range val {
			for _, value := range values {
				newVal[infraType] = append(newVal[infraType], strings.Split(value, " "))
			}
		}
		str = lib.Pformat(newVal)
	}
	fmt.Println(str)
}
