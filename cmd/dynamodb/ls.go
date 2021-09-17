package cliaws

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["dynamodb-ls"] = dynamodbLs
	lib.Args["dynamodb-ls"] = dynamodbLsArgs{}
}

type dynamodbLsArgs struct {
	Status bool `arg:"-s,--status" help:"show table status"`
}

func (dynamodbLsArgs) Description() string {
	return "\nlist dynamodb tables\n"
}

func dynamodbLs() {
	var args dynamodbLsArgs
	arg.MustParse(&args)
	ctx := context.Background()
	tables, err := lib.DynamoDBListTables(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	fmt.Fprintln(os.Stderr, "name status")
	for _, table := range tables {
		if args.Status {
			description, err := lib.DynamoDBClient().DescribeTable(&dynamodb.DescribeTableInput{
				TableName: aws.String(table),
			})
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}
			fmt.Println(table, *description.Table.TableStatus)
		} else {
			fmt.Println(table)
		}
	}
}
