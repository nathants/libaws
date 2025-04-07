package main

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/alexflint/go-arg"
	_ "github.com/nathants/libaws/cmd/acm"
	_ "github.com/nathants/libaws/cmd/api"
	_ "github.com/nathants/libaws/cmd/aws"
	_ "github.com/nathants/libaws/cmd/cloudwatch"
	_ "github.com/nathants/libaws/cmd/codecommit"
	_ "github.com/nathants/libaws/cmd/cost"
	_ "github.com/nathants/libaws/cmd/creds"
	_ "github.com/nathants/libaws/cmd/dynamodb"
	_ "github.com/nathants/libaws/cmd/ec2"
	_ "github.com/nathants/libaws/cmd/ecr"
	_ "github.com/nathants/libaws/cmd/events"
	_ "github.com/nathants/libaws/cmd/iam"
	_ "github.com/nathants/libaws/cmd/infra"
	_ "github.com/nathants/libaws/cmd/lambda"
	_ "github.com/nathants/libaws/cmd/logs"
	_ "github.com/nathants/libaws/cmd/organizations"
	_ "github.com/nathants/libaws/cmd/route53"
	_ "github.com/nathants/libaws/cmd/s3"
	_ "github.com/nathants/libaws/cmd/ses"
	_ "github.com/nathants/libaws/cmd/sqs"
	_ "github.com/nathants/libaws/cmd/ssh"
	_ "github.com/nathants/libaws/cmd/vpc"

	"github.com/nathants/libaws/lib"
)

func usage() {
	var fns []string
	maxLen := 0
	for fn := range lib.Commands {
		fns = append(fns, fn)
		maxLen = lib.Max(maxLen, len(fn))
	}
	sort.Strings(fns)
	fmtStr := "%-" + fmt.Sprint(maxLen) + "s %s\n"
	for _, fn := range fns {
		args := lib.Args[fn]
		val := reflect.ValueOf(args)
		newVal := reflect.New(val.Type())
		newVal.Elem().Set(val)
		p, err := arg.NewParser(arg.Config{}, newVal.Interface())
		if err != nil {
			fmt.Println("Error creating parser:", err)
			return
		}
		var buffer bytes.Buffer
		p.WriteHelp(&buffer)
		descr := buffer.String()
		lines := strings.Split(descr, "\n")
		var line string
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if strings.HasPrefix(l, "Usage:") {
				line = l
			}
		}
		line = strings.ReplaceAll(line, "Usage: libaws", "")
		fmt.Printf(fmtStr, fn, line)
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
		fmt.Fprintln(os.Stderr, "\nunknown command:", cmd)
		os.Exit(1)
	}
	os.Args = os.Args[1:]
	fn()
}
