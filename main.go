package main

import (
	"fmt"
	_ "github.com/nathants/cli-aws/cmd/aws"
	_ "github.com/nathants/cli-aws/cmd/codecommit"
	_ "github.com/nathants/cli-aws/cmd/dynamodb"
	_ "github.com/nathants/cli-aws/cmd/ec2"
	_ "github.com/nathants/cli-aws/cmd/ecr"
	_ "github.com/nathants/cli-aws/cmd/iam"
	_ "github.com/nathants/cli-aws/cmd/route53"
	_ "github.com/nathants/cli-aws/cmd/s3"
	_ "github.com/nathants/cli-aws/cmd/sqs"
	"github.com/nathants/cli-aws/lib"
	"os"
	"sort"
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
		if len(a) > 2 && a[0] == '-' && a[1] != '-' {
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
