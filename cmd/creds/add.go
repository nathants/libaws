package cliaws

import (
	"fmt"
	"io/ioutil"
	"os/user"
	"path"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["creds-add"] = credsAdd
}

type credsAddArgs struct {
	Name            string `arg:"positional,required"`
	AccessKeyID     string `arg:"positional,required"`
	SecretAccessKey string `arg:"positional,required"`
	DefaultRegion   string `arg:"positional,required"`
}

func (credsAddArgs) Description() string {
	return `
    easily switch between aws creds environment variables stored at ~/.aws_creds/

    add new credentials:

        cli-aws creds-add NAME KEY_ID KEY_SECRET REGION

    setup your bashrc:

        source ~/.aws_creds/_temp_creds.sh

    define bash helper functions:

        aws-creds() {
            # permanently set aws credentials for this and future terminal sessions
            cli-aws creds-set $1 && . ~/.aws_creds/_temp_creds.sh
        }

        aws-creds-temp() {
            # temporarily set aws credentials for the current terminal session
            . ~/.aws_creds/$1.sh
            export AWS_CREDS_NAME=$(echo $1|cut -d. -f1)
        }

    AWS_CREDS_NAME=NAME is exported by _temp_creds.sh automatically
`
}

const template = `#!/bin/bash
AWS_ACCESS_KEY_ID=%s
AWS_SECRET_ACCESS_KEY=%s
AWS_DEFAULT_REGION=%s
`

func credsAdd() {
	var args credsAddArgs
	arg.MustParse(&args)
	usr, err := user.Current()
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	home := usr.HomeDir
	root := path.Join(home, ".aws_creds")
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	if strings.Contains(args.Name, " ") {
		lib.Logger.Fatal("creds name cannot contain spaces:", args.Name)
	}
	pth := path.Join(root, args.Name+".sh")
	if lib.Exists(pth) {
		lib.Logger.Fatal("creds with name already exists:", pth)
	}
	contents := fmt.Sprintf(template, args.AccessKeyID, args.SecretAccessKey, args.DefaultRegion)
	err = ioutil.WriteFile(pth, []byte(contents), 0666)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
