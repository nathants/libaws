package cliaws

import (
	"context"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["lambda-rm"] = lambdaRm
	lib.Args["lambda-rm"] = lambdaRmArgs{}
}

type lambdaRmArgs struct {
	Path       string `arg:"positional,required"`
	Preview    bool   `arg:"-p,--preview"`
	Everything bool   `arg:"-e,--everything"`
	Function   bool   `arg:"--function"`
	Role       bool   `arg:"--role"`
	Trigger    bool   `arg:"--trigger"`
	Log        bool   `arg:"--log"`
	S3         bool   `arg:"--s3"`
	SQS        bool   `arg:"--sqs"`
	DynamoDB   bool   `arg:"--dynamodb"`
}

func (lambdaRmArgs) Description() string {
	return "\nlambda rm\n"
}

func lambdaRm() {
	var args lambdaRmArgs
	arg.MustParse(&args)
	ctx := context.Background()

	// preview everything by default
	if !args.Preview && !(args.Everything || args.Function || args.Role || args.Trigger || args.Log || args.S3 || args.SQS || args.DynamoDB) {
		args.Preview = true
		args.Everything = true
	}

	name, err := lib.LambdaName(args.Path)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}

	metadata, err := lib.LambdaParseFile(args.Path)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}

	arnLambda, _ := lib.LambdaArn(ctx, name)

	if arnLambda != "" && args.Everything || args.Trigger {
		metadata.Trigger = []string{}
		err := lib.LambdaEnsureTriggerApi(ctx, name, metadata, args.Preview)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		err = lib.LambdaEnsureTriggerS3(ctx, name, arnLambda, metadata, args.Preview)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		err = lib.LambdaEnsureTriggerCloudwatch(ctx, name, arnLambda, metadata, args.Preview)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		err = lib.LambdaEnsureTriggerDynamoDB(ctx, name, arnLambda, metadata, args.Preview)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		err = lib.LambdaEnsureTriggerSQS(ctx, name, arnLambda, metadata, args.Preview)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
	}

	if arnLambda != "" && args.Everything || args.Role {
		err := lib.IamDeleteRolePolicies(ctx, name, args.Preview)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		err = lib.IamDeleteRoleAllows(ctx, name, args.Preview)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		err = lib.IamDeleteRole(ctx, name, args.Preview)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
	}

	if arnLambda != "" && args.Everything || args.Function {
		err := lib.LambdaDeleteFunction(ctx, name, args.Preview)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
	}

	if args.Everything || args.Log {
		err := lib.LogsDeleteGroup(ctx, "/aws/lambda/"+name, args.Preview)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
	}

	if args.Everything || args.S3 {
		for _, line := range metadata.S3 {
			bucket := strings.Split(line, " ")[0]
			err := lib.S3DeleteBucket(ctx, bucket, args.Preview)
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}
		}
	}

	if args.Everything || args.DynamoDB {
		for _, line := range metadata.DynamoDB {
			table := strings.Split(line, " ")[0]
			err := lib.DynamoDBDeleteTable(ctx, table, args.Preview)
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}
		}
	}

	if args.Everything || args.SQS {
		for _, line := range metadata.DynamoDB {
			queue := strings.Split(line, " ")[0]
			err := lib.SQSDeleteQueue(ctx, queue, args.Preview)
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}
		}
	}

}
