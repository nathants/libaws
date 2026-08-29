package libaws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/alexflint/go-arg"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"

	"github.com/nathants/libaws/lib"
)

func init() {
	lib.Commands["logs-events"] = logsEvents
	lib.Args["logs-events"] = logsEventsArgs{}
}

type logsEventsArgs struct {
	Name        string `arg:"positional,required" help:"log group name"`
	Filter      string `arg:"positional,required" help:"CloudWatch filter pattern"`
	StartMillis int64  `arg:"--start-millis,required" help:"inclusive event-time lower bound in UTC milliseconds"`
	EndMillis   *int64 `arg:"--end-millis" help:"optional exclusive event-time upper bound in UTC milliseconds"`
	MaxEvents   *int64 `arg:"--max-events" help:"optional maximum events; fail if the result exceeds it"`
}

func (logsEventsArgs) Description() string {
	return "\nstream exact filtered CloudWatch events as JSON lines\n"
}

type logsEventRecord struct {
	EventID       string `json:"eventId"`
	IngestionTime int64  `json:"ingestionTime"`
	LogStreamName string `json:"logStreamName"`
	Message       string `json:"message"`
	Timestamp     int64  `json:"timestamp"`
}

type logsEventsClient interface {
	FilterLogEvents(
		context.Context,
		*cloudwatchlogs.FilterLogEventsInput,
		...func(*cloudwatchlogs.Options),
	) (*cloudwatchlogs.FilterLogEventsOutput, error)
}

func writeLogsEvents(
	ctx context.Context,
	client logsEventsClient,
	destination io.Writer,
	name string,
	filter string,
	startMillis int64,
	endMillis *int64,
	maxEvents *int64,
) error {
	if name == "" {
		return errors.New("log group name must not be empty")
	}
	if filter == "" {
		return errors.New("CloudWatch filter pattern must not be empty")
	}
	if startMillis < 0 {
		return errors.New("start milliseconds must not be negative")
	}
	if endMillis != nil && *endMillis < 0 {
		return errors.New("end milliseconds must not be negative")
	}
	if endMillis != nil && *endMillis <= startMillis {
		return errors.New("end milliseconds must be greater than start milliseconds")
	}
	if maxEvents != nil && *maxEvents <= 0 {
		return errors.New("maximum events must be positive")
	}
	if destination == nil {
		return errors.New("output writer must not be nil")
	}

	var inclusiveEndMillis *int64
	if endMillis != nil {
		inclusiveEndMillis = aws.Int64(*endMillis - 1)
	}
	encoder := json.NewEncoder(destination)
	var emitted int64
	var token *string
	seenTokens := map[string]struct{}{}
	for {
		var requestLimit *int32
		if maxEvents != nil {
			const cloudWatchMaxPageEvents int64 = 10_000
			remaining := *maxEvents - emitted
			limit := cloudWatchMaxPageEvents
			if remaining < cloudWatchMaxPageEvents {
				limit = remaining + 1
			}
			requestLimit = aws.Int32(int32(limit))
		}
		out, err := client.FilterLogEvents(ctx, &cloudwatchlogs.FilterLogEventsInput{
			EndTime:       inclusiveEndMillis,
			FilterPattern: aws.String(filter),
			Limit:         requestLimit,
			LogGroupName:  aws.String(name),
			NextToken:     token,
			StartTime:     aws.Int64(startMillis),
		})
		if err != nil {
			return fmt.Errorf("filter CloudWatch log events: %w", err)
		}
		if out == nil {
			return errors.New("CloudWatch returned a nil FilterLogEvents response")
		}
		for _, event := range out.Events {
			if maxEvents != nil && emitted >= *maxEvents {
				return fmt.Errorf("result exceeds --max-events %d", *maxEvents)
			}
			if event.EventId == nil || *event.EventId == "" ||
				event.IngestionTime == nil || event.LogStreamName == nil || *event.LogStreamName == "" ||
				event.Message == nil || event.Timestamp == nil {
				return errors.New("CloudWatch returned a filtered event without complete identity")
			}
			record := logsEventRecord{
				EventID:       *event.EventId,
				IngestionTime: *event.IngestionTime,
				LogStreamName: *event.LogStreamName,
				Message:       *event.Message,
				Timestamp:     *event.Timestamp,
			}
			if err := encoder.Encode(record); err != nil {
				return fmt.Errorf("write CloudWatch log event: %w", err)
			}
			emitted++
		}
		if out.NextToken == nil {
			return nil
		}
		nextToken := *out.NextToken
		if nextToken == "" {
			return errors.New("CloudWatch returned an empty pagination token")
		}
		if _, exists := seenTokens[nextToken]; exists {
			return fmt.Errorf("CloudWatch repeated pagination token %q", nextToken)
		}
		seenTokens[nextToken] = struct{}{}
		token = out.NextToken
	}
}

func logsEvents() {
	var args logsEventsArgs
	arg.MustParse(&args)
	err := writeLogsEvents(
		context.Background(),
		lib.LogsClient(),
		os.Stdout,
		args.Name,
		args.Filter,
		args.StartMillis,
		args.EndMillis,
		args.MaxEvents,
	)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
}
