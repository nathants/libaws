package libaws

import (
	"context"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["dynamodb-item-scan"] = dynamodbItemScan
	lib.Args["dynamodb-item-scan"] = dynamodbItemScanArgs{}
}

type dynamodbItemScanArgs struct {
	Table    string `arg:"positional,required"`
	Limit    int    `arg:"-l,--limit" default:"0" help:"maximum items to print; zero is unlimited"`
	PageSize int    `arg:"--page-size" default:"1000" help:"maximum items evaluated per request"`
}

func (dynamodbItemScanArgs) Description() string {
	return "\nscan dynamodb table\n"
}

func dynamodbItemScan() {
	var args dynamodbItemScanArgs
	arg.MustParse(&args)
	if err := lib.DynamoDBItemScan(
		context.Background(),
		args.Table,
		args.Limit,
		args.PageSize,
		os.Stdout,
	); err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
