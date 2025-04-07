package libaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["s3-ensure"] = s3Ensure
	lib.Args["s3-ensure"] = s3EnsureArgs{}
}

type s3EnsureArgs struct {
	Name    string   `arg:"positional,required"`
	Attr    []string `arg:"positional"`
	Preview bool     `arg:"-p,--preview"`
}

func (s3EnsureArgs) Description() string {
	return `
ensure a s3 bucket

example:
 - libaws s3-ensure test-bucket acl=public versioning=true

optional attrs:
 - acl=VALUE        (values = public | private, default = private)
 - versioning=VALUE (values = true | false,     default = false)
 - metrics=VALUE    (values = true | false,     default = false)
 - cors=VALUE       (values = true | false,     default = false)
 - ttldays=VALUE    (values = 0 | n,            default = 0)
 - allow_put=VALUE  (values = $principal.amazonaws.com)

setting 'cors=true' uses '*' for allowed origins. to specify one or more explicit origins, do this instead:
 - corsorigin=http://localhost:8080
 - corsorigin=https://example.com

note: bucket acl can only be set at bucket creation time

`
}

func s3Ensure() {
	var args s3EnsureArgs
	arg.MustParse(&args)
	ctx := context.Background()
	input, err := lib.S3EnsureInput("", args.Name, args.Attr)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	err = lib.S3Ensure(ctx, input, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
