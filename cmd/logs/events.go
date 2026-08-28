package libaws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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
	StartMillis int64  `arg:"--start-millis" default:"-1" help:"optional inclusive event-time lower bound in UTC milliseconds"`
	EndMillis   int64  `arg:"--end-millis" default:"-1" help:"optional exclusive event-time upper bound in UTC milliseconds"`
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

func readLogsEvents(
	ctx context.Context,
	client logsEventsClient,
	name string,
	filter string,
	startMillis *int64,
	endMillis *int64,
) ([]logsEventRecord, error) {
	if name == "" {
		return nil, errors.New("log group name must not be empty")
	}
	if filter == "" {
		return nil, errors.New("CloudWatch filter pattern must not be empty")
	}
	if startMillis != nil && *startMillis < 0 {
		return nil, errors.New("start milliseconds must not be negative")
	}
	if endMillis != nil && *endMillis < 0 {
		return nil, errors.New("end milliseconds must not be negative")
	}
	if startMillis != nil && endMillis != nil && *endMillis <= *startMillis {
		return nil, errors.New("end milliseconds must be greater than start milliseconds")
	}

	var records []logsEventRecord
	var token *string
	seenTokens := map[string]struct{}{}
	for {
		out, err := client.FilterLogEvents(ctx, &cloudwatchlogs.FilterLogEventsInput{
			EndTime:       endMillis,
			FilterPattern: aws.String(filter),
			LogGroupName:  aws.String(name),
			NextToken:     token,
			StartTime:     startMillis,
		})
		if err != nil {
			return nil, err
		}
		if out == nil {
			return nil, errors.New("CloudWatch returned a nil FilterLogEvents response")
		}
		for _, event := range out.Events {
			if event.EventId == nil || *event.EventId == "" ||
				event.IngestionTime == nil || event.LogStreamName == nil || *event.LogStreamName == "" ||
				event.Message == nil || event.Timestamp == nil {
				return nil, errors.New("CloudWatch returned a filtered event without complete identity")
			}
			records = append(records, logsEventRecord{
				EventID:       *event.EventId,
				IngestionTime: *event.IngestionTime,
				LogStreamName: *event.LogStreamName,
				Message:       *event.Message,
				Timestamp:     *event.Timestamp,
			})
		}
		if out.NextToken == nil {
			return records, nil
		}
		nextToken := *out.NextToken
		if nextToken == "" {
			return nil, errors.New("CloudWatch returned an empty pagination token")
		}
		if _, exists := seenTokens[nextToken]; exists {
			return nil, fmt.Errorf("CloudWatch repeated pagination token %q", nextToken)
		}
		seenTokens[nextToken] = struct{}{}
		token = out.NextToken
	}
}

func logsEvents() {
	var args logsEventsArgs
	arg.MustParse(&args)
	var startMillis *int64
	if args.StartMillis != -1 {
		startMillis = &args.StartMillis
	}
	var endMillis *int64
	if args.EndMillis != -1 {
		endMillis = &args.EndMillis
	}
	records, err := readLogsEvents(
		context.Background(),
		lib.LogsClient(),
		args.Name,
		args.Filter,
		startMillis,
		endMillis,
	)
	if err != nil {
		lib.Logger.Fatal("error: ", err)
	}
	for _, record := range records {
		data, err := json.Marshal(record)
		if err != nil {
			lib.Logger.Fatal("error: ", err)
		}
		fmt.Println(string(data))
	}
}
