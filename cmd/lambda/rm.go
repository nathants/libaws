package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["lambda-rm"] = lambdaRm
	lib.Args["lambda-rm"] = lambdaRmArgs{}
}

type lambdaRmArgs struct {
	Name    string `arg:"positional,required"`
	Preview bool   `arg:"-p,--preview"`
}

func (lambdaRmArgs) Description() string {
	return "\nlambda rm\n"
}

func lambdaRm() {
	var args lambdaRmArgs
	arg.MustParse(&args)
	ctx := context.Background()
	triggerChan := make(chan *lib.InfraTrigger)
	close(triggerChan)
	infraLambdas, err := lib.InfraListLambda(ctx, triggerChan, args.Name)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for lambdaName, infraLambda := range infraLambdas {
		if lambdaName != args.Name {
			continue
		}
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

}
