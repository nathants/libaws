package cliaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["logs-set-retention-all"] = logsSetRetentionALl
	lib.Args["logs-set-retention-all"] = logsSetRetentionALlArgs{}
}

type logsSetRetentionALlArgs struct {
	Days int64 `arg:"positional,required" help:"days to retain log data"`
}

func (logsSetRetentionALlArgs) Description() string {
	return "\nset log retention days for all log groups\n"
}

func logsSetRetentionALl() {
	var args logsSetRetentionALlArgs
	arg.MustParse(&args)
	ctx := context.Background()
	logs, err := lib.LogsListLogGroups(ctx)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, log := range logs {
		old := 0
		if log.RetentionInDays != nil {
			old = int(*log.RetentionInDays)
		}
		_, err := lib.LogsClient().PutRetentionPolicyWithContext(ctx, &cloudwatchlogs.PutRetentionPolicyInput{
			LogGroupName:    log.LogGroupName,
			RetentionInDays: aws.Int64(args.Days),
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		lib.Logger.Printf("set retention from %d to %d days for %s\n", old, args.Days, *log.LogGroupName)
	}
}
