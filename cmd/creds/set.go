package cliaws

import (
	"fmt"
	"os"
	"os/user"
	"path"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["creds-set"] = credsSet
	lib.Args["creds-set"] = credsSetArgs{}
}

type credsSetArgs struct {
	Name string `arg:"positional,required"`
}

func (credsSetArgs) Description() string {
	return `switch between aws creds`
}

func credsSet() {
	var args credsSetArgs
	arg.MustParse(&args)
	usr, err := user.Current()
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	home := usr.HomeDir
	dir := os.Getenv("LIBAWS_CREDS_DIR")
	if dir == "" {
		dir = "secure/aws_creds"
	}
	root := path.Join(home, dir)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".creds") {
			continue
		}
		name := strings.Split(entry.Name(), ".")[0]
		if name == args.Name {
			err = os.MkdirAll(path.Join(home, ".aws"), 0700)
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}
			_ = os.Remove(path.Join(home, ".aws", "credentials"))
			err := os.Symlink(path.Join(root, name+".creds"), path.Join(home, ".aws", "credentials"))
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}
			_ = os.Remove(path.Join(home, ".aws", "config"))
			err = os.Symlink(path.Join(root, name+".config"), path.Join(home, ".aws", "config"))
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}
			os.Exit(0)
		}

	}
	fmt.Println("no creds for name:", args.Name)
	os.Exit(1)
}
