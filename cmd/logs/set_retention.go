package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["logs-set-retention"] = logsSetRetention
	lib.Args["logs-set-retention"] = logsSetRetentionArgs{}
}

type logsSetRetentionArgs struct {
	Name string `arg:"positional,required" help:"log group name"`
	Days int64  `arg:"positional,required" help:"days to retain log data"`
}

func (logsSetRetentionArgs) Description() string {
	return "\nset log retention days\n"
}

func logsSetRetention() {
	var args logsSetRetentionArgs
	arg.MustParse(&args)
	ctx := context.Background()
	_, err := lib.LogsClient().PutRetentionPolicyWithContext(ctx, &cloudwatchlogs.PutRetentionPolicyInput{
		LogGroupName:    aws.String(args.Name),
		RetentionInDays: aws.Int64(args.Days),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	lib.Logger.Printf("set retention to %d days for %s\n", args.Days, args.Name)
}
