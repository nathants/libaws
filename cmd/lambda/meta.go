package cliaws

import (
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["lambda-meta"] = lambdaMeta
	lib.Args["lambda-meta"] = lambdaMetaArgs{}
}

type lambdaMetaArgs struct {
	Path string `arg:"positional,required"`
}

func (lambdaMetaArgs) Description() string {
	return "\nget lambda meta\n"
}

func lambdaMeta() {
	var args lambdaMetaArgs
	arg.MustParse(&args)
	if !lib.Exists(args.Path) {
		err := fmt.Errorf("no such file: %s", args.Path)
		lib.Logger.Fatal("error:", err)
	}
	if !strings.HasSuffix(args.Path, ".py") && !strings.HasSuffix(args.Path, ".go") {
		err := fmt.Errorf("only .py or .go files supported: %s", args.Path)
		lib.Logger.Fatal("error:", err)
	}
	data, err := ioutil.ReadFile(args.Path)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	meta, err := lib.LambdaGetMetadata(strings.Split(string(data), "\n"))
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(lib.Pformat(meta))
}
