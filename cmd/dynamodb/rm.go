package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["dynamodb-rm"] = dynamodbRm
}

type dynamodbRmArgs struct {
	Name    string `arg:"positional,required"`
	Preview bool   `arg:"-p,--preview"`
}

func (dynamodbRmArgs) Description() string {
	return "\nlist dynamodb tables\n"
}

func dynamodbRm() {
	var args dynamodbRmArgs
	arg.MustParse(&args)
	ctx := context.Background()
	err := lib.DynamoDBDeleteTable(ctx, args.Name, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
