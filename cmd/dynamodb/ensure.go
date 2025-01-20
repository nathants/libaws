package cliaws

import (
	"context"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["dynamodb-ensure"] = dynamodbEnsure
	lib.Args["dynamodb-ensure"] = dynamodbEnsureArgs{}
}

type dynamodbEnsureArgs struct {
	Name    string   `arg:"positional,required"`
	Attr    []string `arg:"positional,required"`
	Preview bool     `arg:"-p,--preview"`
}

func (dynamodbEnsureArgs) Description() string {
	return `
ensure a dynamodb table

example:

 - libaws dynamodb-ensure test-table userid:s:hash timestamp:n:range stream=keys_only

 - libaws dynamodb-ensure test-table \
      username:s:hash \
      GlobalSecondaryIndexes.0.IndexName=testIndex \
      GlobalSecondaryIndexes.0.Projection.ProjectionType=ALL \
      GlobalSecondaryIndexes.0.Key.0=hometown:s:hash \

required attrs:

 - NAME:ATTR_TYPE:KEY_TYPE

optional attrs:

 - ProvisionedThroughput.ReadCapacityUnits=VALUE, shortcut: read=VALUE,  default: 0
 - ProvisionedThroughput.WriteCapacityUnits=VALUE shortcut: write=VALUE, default: 0
 - StreamSpecification.StreamViewType=VALUE,      shortcut: stream=VALUE

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

 - ttl=ATTR_NAME
`
}

func dynamodbEnsure() {
	var args dynamodbEnsureArgs
	arg.MustParse(&args)
	ctx := context.Background()
	var keys []string
	var attrs []string
	for _, param := range args.Attr {
		if strings.Contains(param, "=") {
			attrs = append(attrs, param)
		} else {
			keys = append(keys, param)
		}
	}
	input, ttl, err := lib.DynamoDBEnsureInput("", args.Name, keys, attrs)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	err = lib.DynamoDBEnsure(ctx, input, ttl, args.Preview)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
