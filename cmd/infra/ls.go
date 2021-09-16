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
lib.Args["infra-ls"] = infraLsArgs{}
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
	// split attrs on space, string -> []string
	if args.SplitSpaces {
		val := make(map[string]map[string]string)
		newVal := make(map[string]map[string][]string)
		err := json.Unmarshal([]byte(str), &val)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		for infraType, values := range val {
			newVal[infraType] = make(map[string][]string)
			for ks, vs := range values {
				newVal[infraType][ks] = strings.Split(vs, " ")
			}
		}
		fmt.Println(lib.Pformat(newVal))
		return
	}
	// justify map values for readability
	indent := 0
	var vals []string
	for _, line := range strings.Split(str, "\n") {
		var i int
		var c rune
		for i, c = range line {
			if c != ' ' {
				break
			}
		}
		if i != indent {
			justify := true
			for _, line := range vals {
				if !strings.Contains(line, ":") {
					justify = false
				}
			}
			if justify {
				maxHeader := 0
				for _, line := range vals {
					parts := strings.SplitN(line, ":", 2)
					maxHeader = max(maxHeader, len(parts[0]))
				}
				for _, line := range vals {
					parts := strings.SplitN(line, ":", 2)
					fmt.Print(parts[0] + ":")
					for i := 0; i < maxHeader - len(parts[0]); i++ {
						fmt.Print(" ")
					}
					fmt.Print(parts[1])
					fmt.Print("\n")
				}
			} else {
				for _, line := range vals {
					fmt.Println(line)
				}
			}
			vals = nil
			indent = i
		}
		vals = append(vals, line)
	}
	for _, line := range vals {
		fmt.Println(line)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
