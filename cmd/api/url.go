package cliaws

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["api-url"] = apiUrl
	lib.Args["api-url"] = apiUrlArgs{}
}

type apiUrlArgs struct {
	Name string `arg:"positional,required"`
}

func (apiUrlArgs) Description() string {
	return "\nget api url\n"
}

func apiUrl() {
	var args apiUrlArgs
	arg.MustParse(&args)
	ctx := context.Background()
	url, err := lib.ApiUrl(ctx, args.Name)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Println(url)
}
