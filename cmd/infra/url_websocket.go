package cliaws

import (
	"context"
	"fmt"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["infra-url-websocket"] = infraUrlWebsocket
	lib.Args["infra-url-websocket"] = infraUrlWebsocketArgs{}
}

type infraUrlWebsocketArgs struct {
	YamlPath   string `arg:"positional,required"`
	LambdaName string `arg:"positional,required"`
}

func (infraUrlWebsocketArgs) Description() string {
	return "\nget infra websocket url\n"
}

func infraUrlWebsocket() {
	var args infraUrlWebsocketArgs
	arg.MustParse(&args)
	ctx := context.Background()
	infraSet, err := lib.InfraParse(args.YamlPath)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for name := range infraSet.Lambda {
		if name == args.LambdaName {
			url, err := lib.ApiUrl(ctx, name+lib.LambdaWebsocketSuffix)
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}
			fmt.Println(url)
			os.Exit(0)
		}
	}
	os.Exit(1)
}
