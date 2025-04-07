package libaws

import (
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
	"gopkg.in/yaml.v3"
)

func init() {
	lib.Commands["infra-parse"] = infraParse
	lib.Args["infra-parse"] = infraParseArgs{}
}

type infraParseArgs struct {
	YamlPath string `arg:"positional"`
}

func (infraParseArgs) Description() string {
	return "\nparse and validate an infra.yaml file\n"
}

func infraParse() {
	var args infraParseArgs
	arg.MustParse(&args)
	infra, err := lib.InfraParse(args.YamlPath)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	data, err := yaml.Marshal(infra)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(string(data))
}
