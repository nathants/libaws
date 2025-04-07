package libaws

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
		fmt.Println(name)
	}

}
