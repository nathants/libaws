package cliaws

import (
	"fmt"
	"io/ioutil"
	"os/user"
	"path"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["creds-add"] = credsAdd
	lib.Args["creds-add"] = credsAddArgs{}
}

type credsAddArgs struct {
	Name            string `arg:"positional,required"`
	AccessKeyID     string `arg:"positional,required"`
	SecretAccessKey string `arg:"positional,required"`
	DefaultRegion   string `arg:"positional,required"`
}

func (credsAddArgs) Description() string {
	return `
add aws creds

    add new credentials:

        libaws creds-add NAME KEY_ID KEY_SECRET REGION

    add to your bashrc:

        source ~/.aws_creds/_temp_creds.sh

        aws-creds() {
            # permanently set aws credentials for this and future terminal sessions
            libaws creds-set $1 && source ~/.aws_creds/_temp_creds.sh
        }

        aws-creds-temp() {
            # temporarily set aws credentials for the current terminal session
            source ~/.aws_creds/$1.sh
            export AWS_CREDS_NAME=$(echo $1|cut -d. -f1)
        }

`
}

const template = `#!/bin/bash
export AWS_STS_REGIONAL_ENDPOINTS=regional
export AWS_SDK_LOAD_CONFIG=true
export AWS_ACCESS_KEY_ID=%s
export AWS_SECRET_ACCESS_KEY=%s
export AWS_DEFAULT_REGION=%s
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
