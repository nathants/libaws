package cliaws

import (
	"context"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["dynamodb-ensure"] = dynamodbEnsure
}

type dynamodbEnsureArgs struct {
	Name    string   `arg:"positional,required"`
	Attrs   []string `arg:"positional,required"`
	Preview bool     `arg:"-p,--preview"`
}

func (dynamodbEnsureArgs) Description() string {
	return `
ensure a dynamodb table

example:
 - cli-aws dynamodb-ensure test-table userid:s:hash timestamp:n:range Tags.0.Key=foo Tags.0.Key=bar

required attrs:
 - NAME:ATTR_TYPE:KEY_TYPE

optional attrs:
 - SSESpecification.KMSMasterKeyId=VALUE

 - ProvisionedThroughput.ReadCapacityUnits=VALUE
 - ProvisionedThroughput.WriteCapacityUnits=VALUE

 - StreamSpecification.StreamViewType=VALUE

 - LocalSecondaryIndexes.INTEGER.IndexName=VALUE
 - LocalSecondaryIndexes.INTEGER.Key.INTEGER=NAME:ATTR_TYPE:KEY_TYPE
 - LocalSecondaryIndexes.INTEGER.Projection.ProjectionType=VALUE
 - LocalSecondaryIndexes.INTEGER.Projection.NonKeyAttributes.INTEGER=VALUE

 - GlobalSecondaryIndexes.INTEGER.IndexName=VALUE
 - GlobalSecondaryIndexes.INTEGER.Key.INTEGER=NAME:ATTR_TYPE:KEY_TYPE
 - GlobalSecondaryIndexes.INTEGER.Projection.ProjectionType=VALUE
 - GlobalSecondaryIndexes.INTEGER.Projection.NonKeyAttributes.INTEGER=VALUE
 - GlobalSecondaryIndexes.INTEGER.ProvisionedThroughput.ReadCapacityUnits=VALUE
 - GlobalSecondaryIndexes.INTEGER.ProvisionedThroughput.WriteCapacityUnits=VALUE

 - Tags.KEY=VALUE
`
}

func dynamodbEnsure() {
	var args dynamodbEnsureArgs
	arg.MustParse(&args)
	ctx := context.Background()
	var keys []string
	var attrs []string
	for _, param := range args.Attrs {
		if strings.Contains(param, "=") {
			attrs = append(attrs, param)
		} else {
			keys = append(keys, param)
		}
	}
	input, err := lib.DynamoDBEnsureInput(args.Name, keys, attrs)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	err = lib.DynamoDBEnsure(ctx, input, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
