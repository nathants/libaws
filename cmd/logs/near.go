package libaws

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"

	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["logs-near"] = logsNear
	lib.Args["logs-near"] = logsNearArgs{}
}

type logsNearArgs struct {
	Name      string `arg:"positional,required" help:"log group name"`
	Timestamp int64  `arg:"positional,required" help:"utc millis timestamp at which to show logs"`
	Context   int64  `arg:"-c,--context" help:"millis context around timestamp" default:"30000"`
	Max       int    `arg:"-m,--max" default:"64" help:"max results"`
}

func (logsNearArgs) Description() string {
	return "\nshow all logs within CONTEXT millis of TIMESTAMP\n"
}

func logsNear() {
	var args logsNearArgs
	arg.MustParse(&args)
	ctx := context.Background()
	count := 0
	var token *string
	for {
		out, err := lib.LogsClient().FilterLogEvents(ctx, &cloudwatchlogs.FilterLogEventsInput{
			EndTime:       aws.Int64(args.Timestamp + args.Context),
			StartTime:     aws.Int64(args.Timestamp - args.Context),
			FilterPattern: aws.String(""),
			LogGroupName:  aws.String(args.Name),
			NextToken:     token,
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		for _, e := range out.Events {
			val := map[string]any{}
			err := json.Unmarshal([]byte(*e.Message), &val)
			if err != nil {
				fmt.Printf("timestamp=%d %s", *e.Timestamp, *e.Message)
			} else {
				val["timestamp"] = *e.Timestamp
				bytes, err := json.Marshal(val)
				if err != nil {
					lib.Logger.Fatal("error: ", err)
				}
				fmt.Println(string(bytes))
			}
			count++
			if count >= args.Max {
				return
			}
		}
		if out.NextToken == nil {
			break
		}
		token = out.NextToken
	}
}
