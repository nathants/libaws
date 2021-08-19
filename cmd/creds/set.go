package cliaws

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"path"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["creds-set"] = credsSet
}

type credsSetArgs struct {
	Name string `arg:"positional,required"`
}

func (credsSetArgs) Description() string {
	return `
    easily switch between aws creds stored as environement vars in files like: ~/.aws_creds/NAME.sh

    NAME.sh should contain at least:
      export AWS_ACCESS_KEY_ID=
      export AWS_SECRET_ACCESS_KEY=
      export AWS_DEFAULT_REGION

    defines env var AWS_CREDS_NAME=NAME when switching

    define bash functions like this to make your life easier:

        aws-creds() {
            cli-aws creds-set $1 && . ~/.aws_creds/_temp_creds.sh
        }

        aws-creds-temp() {
            export AWS_CREDS_NAME=$(echo $1|cut -d. -f1)
            . ~/.aws_creds/$1.sh
        }
`
}

const tempCreds = "_temp_creds.sh"

func credsSet() {
	var args credsSetArgs
	arg.MustParse(&args)
	usr, err := user.Current()
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	home := usr.HomeDir
	root := path.Join(home, ".aws_creds")
	entries, err := os.ReadDir(root)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	var creds []string
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		if entry.Name() == tempCreds {
			continue
		}
		bytes, err := ioutil.ReadFile(path.Join(root, entry.Name()))
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		text := string(bytes)
		if !strings.Contains(text, "export AWS_ACCESS_KEY_ID=") {
			continue
		}
		if !strings.Contains(text, "export AWS_SECRET_ACCESS_KEY=") {
			continue
		}
		if !strings.Contains(text, "export AWS_DEFAULT_REGION=") {
			continue
		}
		creds = append(creds, strings.Split(entry.Name(), ".")[0])
		if args.Name+".sh" != entry.Name() {
			continue
		}
		name := strings.Split(entry.Name(), ".")[0]
		lines := strings.Split(text, "\n")
		text = ""
		for _, line := range lines {
			if strings.Contains(line, "export") {
				text += line + "\n"
			}
		}
		err = ioutil.WriteFile(path.Join(root, tempCreds), []byte("export AWS_CREDS_NAME="+name+"\n"+text), 0666)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		return
	}
	fmt.Println("fatal: no match, try:")
	for _, cred := range creds {
		fmt.Println(" ", cred)
	}
	os.Exit(1)
}
