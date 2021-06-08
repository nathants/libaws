package cliaws

import (
	"context"
	"fmt"

	// "github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["dynamodb-ls"] = dynamodbLs
}

type dynamodbLsArgs struct {
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
	for _, table := range tables {
		description, err := lib.DynamoDBClient().DescribeTable(&dynamodb.DescribeTableInput{
			TableName: table,
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		fmt.Println(*table, *description.Table.TableStatus)
	}
}
