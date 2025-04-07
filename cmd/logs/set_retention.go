package libaws

import (
	"context"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["logs-set-retention"] = logsSetRetention
	lib.Args["logs-set-retention"] = logsSetRetentionArgs{}
}

type logsSetRetentionArgs struct {
	Name string `arg:"positional,required" help:"log group name"`
	Days int32  `arg:"positional,required" help:"days to retain log data"`
}

func (logsSetRetentionArgs) Description() string {
	return "\nset log retention days\n"
}

func logsSetRetention() {
	var args logsSetRetentionArgs
	arg.MustParse(&args)
	ctx := context.Background()
	_, err := lib.LogsClient().PutRetentionPolicy(ctx, &cloudwatchlogs.PutRetentionPolicyInput{
		LogGroupName:    aws.String(args.Name),
		RetentionInDays: aws.Int32(args.Days),
	})
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	lib.Logger.Printf("set retention to %d days for %s\n", args.Days, args.Name)
}
