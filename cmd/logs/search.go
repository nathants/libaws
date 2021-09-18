package cliaws

import (
	"encoding/json"
	"context"
	"fmt"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/nathants/cli-aws/lib"
)

func init() {
	lib.Commands["logs-search"] = logsSearch
	lib.Args["logs-search"] = logsSearchArgs{}
}

type logsSearchArgs struct {
	Name      string `arg:"positional,required" help:"log group name"`
	Query     string `arg:"positional,required" help:"search query"`
	FromHours int    `arg:"-f,--from-hours" default:"12" help:"search logs no older than this"`
	ToHours   int    `arg:"-t,--to-hours" default:"0" help:"search logs no younger than this"`
	Max       int    `arg:"-m,--max" default:"64" help:"max results"`
}

func (logsSearchArgs) Description() string {
	return "\nsearch logs\n"
}

func logsSearch() {
	var args logsSearchArgs
	arg.MustParse(&args)
	ctx := context.Background()
	var toTime *int64
	if args.ToHours != 0 {
		offset := time.Hour * time.Duration(args.ToHours)
		toTime = aws.Int64(time.Now().UTC().UnixMilli() - offset.Milliseconds())
	}
	offset := time.Hour * time.Duration(args.FromHours)
	fromTime := aws.Int64(time.Now().UTC().UnixMilli() - offset.Milliseconds())
	count := 0
	var token *string

	for {
		out, err := lib.LogsClient().FilterLogEventsWithContext(ctx, &cloudwatchlogs.FilterLogEventsInput{
			EndTime:       toTime,
			StartTime:     fromTime,
			FilterPattern: aws.String(args.Query),
			LogGroupName:  aws.String(args.Name),
			NextToken:     token,
		})
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		for _, e := range out.Events {
			val := make(map[string]interface{})
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
