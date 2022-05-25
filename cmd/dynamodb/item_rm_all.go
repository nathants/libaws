package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["dynamodb-item-rm-all"] = dynamodbItemRmAll
	lib.Args["dynamodb-item-rm-all"] = dynamodbItemRmAllArgs{}
}

type dynamodbItemRmAllArgs struct {
	Table string   `arg:"positional"`
	Keys  []string `arg:"positional,required"`
}

func (dynamodbItemRmAllArgs) Description() string {
	return "\nrm all items in dynamodb table\n"
}

func dynamodbItemRmAll() {
	var args dynamodbItemRmAllArgs
	arg.MustParse(&args)
	ctx := context.Background()
	err := lib.DynamoDBItemDeleteAll(ctx, args.Table, args.Keys)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
