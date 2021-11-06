package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["s3-ensure"] = s3Ensure
	lib.Args["s3-ensure"] = s3EnsureArgs{}
}

type s3EnsureArgs struct {
	Name    string   `arg:"positional,required"`
	Attrs   []string `arg:"positional"`
	Preview bool     `arg:"-p,--preview"`
}

func (s3EnsureArgs) Description() string {
	return `
ensure a dynamodb table

example:
 - cli-aws s3-ensure test-bucket acl=PUBLIC versioning=TRUE

optional attrs:
 - acl=VALUE        (values = "public" | "private", default = "private")
 - versioning=VALUE (values = "true" | "false",     default = "false")
 - encryption=VALUE (values = "true" | "false",     default = "true")
 - metrics=VALUE    (values = "true" | "false",     default = "true")
 - cors=VALUE       (values = "true" | "false",     default = "true")
`
}

func s3Ensure() {
	var args s3EnsureArgs
	arg.MustParse(&args)
	ctx := context.Background()
	input, err := lib.S3EnsureInput(args.Name, args.Attrs)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	err = lib.S3Ensure(ctx, input, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
