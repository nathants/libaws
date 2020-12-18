package main

import (
"strings"
	"sort"
	"fmt"
	"os"
	"github.com/nathants/cli-aws/lib"
	_ "github.com/nathants/cli-aws/route53"

)

func usage() {
	var fns []string
	for k := range lib.Commands {
		fns = append(fns, k)
	}
	sort.Strings(fns)
	for _, fn := range fns {
		fmt.Println(fn)
	}
}

func main() {
	if len(os.Args) < 2 || os.Args[1] == "-h" || os.Args[1] == "--help" {
		usage()
		os.Exit(1)
	}
	cmd := os.Args[1]
	fn, ok := lib.Commands[cmd]
	if !ok {
		usage()
		os.Exit(1)
	}
	var args []string
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "-") && len(a) > 2 {
			for _, k := range a[1:] {
				args = append(args, fmt.Sprintf("-%s", string(k)))
			}
		} else {
			args = append(args, a)
		}
	}
	os.Args = args
	fn()
}
