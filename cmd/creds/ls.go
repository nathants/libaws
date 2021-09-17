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
	lib.Commands["creds-ls"] = credsLs
	lib.Args["creds-ls"] = credsLsArgs{}
}

type credsLsArgs struct {
}

func (credsLsArgs) Description() string {
	return "\nlist aws creds\n"
}

func credsLs() {
	var args credsLsArgs
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
	lib.Logger.Println("~/.aws_creds/")
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		if entry.Name() == "_temp_creds.sh" {
			continue
		}
		bytes, err := ioutil.ReadFile(path.Join(root, entry.Name()))
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		text := string(bytes)
		if !strings.Contains(text, "AWS_ACCESS_KEY_ID=") {
			continue
		}
		if !strings.Contains(text, "AWS_SECRET_ACCESS_KEY=") {
			continue
		}
		if !strings.Contains(text, "AWS_DEFAULT_REGION=") {
			continue
		}
		fmt.Println(strings.Split(entry.Name(), ".sh")[0])
	}

}
