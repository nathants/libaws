package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["infra-rm"] = infraRm
	lib.Args["infra-rm"] = infraRmArgs{}
}

type infraRmArgs struct {
	YamlPath string `arg:"positional,required"`
	Preview  bool   `arg:"-p,--preview"`
}

func (infraRmArgs) Description() string {
	return "\ninfra rm\n"
}

func infraRm() {
	var args infraRmArgs
	arg.MustParse(&args)
	ctx := context.Background()
	infraSet, err := lib.InfraParse(args.YamlPath)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for vpcName := range infraSet.Vpc {
		err := lib.VpcRm(ctx, vpcName, args.Preview)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
	}
	for profileName := range infraSet.InstanceProfile {
		err := lib.IamDeleteInstanceProfile(ctx, profileName, args.Preview)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
	}
	for keypairName := range infraSet.Keypair {
		err := lib.EC2DeleteKeypair(ctx, keypairName, args.Preview)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
	}
	for lambdaName, infraLambda := range infraSet.Lambda {
		infraLambda.Name = lambdaName
		infraLambda.Arn, _ = lib.LambdaArn(ctx, lambdaName)
		infraLambda.Trigger = nil
		_, err := lib.LambdaEnsureTriggerApi(ctx, infraLambda, args.Preview)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		if infraLambda.Arn != "" {
			_, err := lib.LambdaEnsureTriggerS3(ctx, infraLambda, args.Preview)
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}
			_, err = lib.LambdaEnsureTriggerEcr(ctx, infraLambda, args.Preview)
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}
			_, err = lib.LambdaEnsureTriggerSchedule(ctx, infraLambda, args.Preview)
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}
			err = lib.LambdaEnsureTriggerDynamoDB(ctx, infraLambda, args.Preview)
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}
			err = lib.LambdaEnsureTriggerSQS(ctx, infraLambda, args.Preview)
			if err != nil {
				lib.Logger.Fatal("error: ", err)
			}
		}
		err = lib.IamDeleteRole(ctx, lambdaName, args.Preview)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		err = lib.LambdaDeleteFunction(ctx, lambdaName, args.Preview)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		err = lib.LogsDeleteGroup(ctx, "/aws/lambda/"+lambdaName, args.Preview)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
	}
	for bucketName := range infraSet.S3 {
		err := lib.S3DeleteBucket(ctx, bucketName, args.Preview)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
	}
	for tableName := range infraSet.DynamoDB {
		err := lib.DynamoDBDeleteTable(ctx, tableName, args.Preview)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
	}
	for queueName := range infraSet.SQS {
		err := lib.SQSDeleteQueue(ctx, queueName, args.Preview)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
	}
}
