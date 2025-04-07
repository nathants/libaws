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
	return `add aws creds`
}

const templateCreds = `[default]
aws_access_key_id=%s
aws_secret_access_key=%s
`

const templateConfig = `[default]
region=%s
output=json
`

func credsAdd() {
	var args credsAddArgs
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
	err = os.MkdirAll(root, 0700)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	if strings.Contains(args.Name, " ") {
		lib.Logger.Fatal("creds name cannot contain spaces:", args.Name)
	}
	pth := path.Join(root, args.Name+".config")
	if lib.Exists(pth) {
		lib.Logger.Fatal("creds with name already exists:", pth)
	}
	contents := fmt.Sprintf(templateConfig, args.DefaultRegion)
	err = os.WriteFile(pth, []byte(contents), 0600)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	lib.Logger.Println("created:", pth)
	pth = path.Join(root, args.Name+".creds")
	if lib.Exists(pth) {
		lib.Logger.Fatal("creds with name already exists:", pth)
	}
	contents = fmt.Sprintf(templateCreds, args.AccessKeyID, args.SecretAccessKey)
	err = os.WriteFile(pth, []byte(contents), 0600)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	lib.Logger.Println("created:", pth)
}
