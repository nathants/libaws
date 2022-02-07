package lib

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
)

var logsClient *cloudwatchlogs.CloudWatchLogs
var logsClientLock sync.RWMutex

func LogsClient() *cloudwatchlogs.CloudWatchLogs {
	logsClientLock.Lock()
	defer logsClientLock.Unlock()
	if logsClient == nil {
		logsClient = cloudwatchlogs.New(Session())
	}
	return logsClient
}

func LogsEnsureGroup(ctx context.Context, name string, preview bool) error {
	_, err := LogsClient().GetLogGroupFieldsWithContext(ctx, &cloudwatchlogs.GetLogGroupFieldsInput{
		LogGroupName: aws.String(name),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != cloudwatchlogs.ErrCodeResourceNotFoundException {
			Logger.Println("error:", err)
			return err
		}
		if !preview {
			_, err := LogsClient().CreateLogGroupWithContext(ctx, &cloudwatchlogs.CreateLogGroupInput{
				LogGroupName: aws.String(name),
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"created log group:", name)
	}
	return nil
}

func LogsListLogGroups(ctx context.Context) ([]*cloudwatchlogs.LogGroup, error) {
	var token *string
	var logs []*cloudwatchlogs.LogGroup
	for {
		out, err := LogsClient().DescribeLogGroupsWithContext(ctx, &cloudwatchlogs.DescribeLogGroupsInput{
			NextToken: token,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		logs = append(logs, out.LogGroups...)
		if out.NextToken == nil {
			break
		}
		token = out.NextToken
	}
	return logs, nil
}

func LogsDeleteGroup(ctx context.Context, name string, preview bool) error {
	_, err := LogsClient().DescribeLogStreamsWithContext(ctx, &cloudwatchlogs.DescribeLogStreamsInput{
		LogGroupName: aws.String(name),
	})
	if err != nil {
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != cloudwatchlogs.ErrCodeResourceNotFoundException {
			Logger.Println("error:", err)
			return err
		}
		return nil
	}
	if !preview {
		_, err := LogsClient().DeleteLogGroupWithContext(ctx, &cloudwatchlogs.DeleteLogGroupInput{
			LogGroupName: aws.String(name),
		})
		aerr, ok := err.(awserr.Error)
		if !ok || aerr.Code() != "ResourceNotFoundException" {
			Logger.Println("error:", err)
			return err
		}
	}
	Logger.Println(PreviewString(preview)+"deleted log group:", name)
	return nil
}

func LogsMostRecentStreams(ctx context.Context, name string) ([]*cloudwatchlogs.LogStream, error) {
	out, err := LogsClient().DescribeLogStreamsWithContext(ctx, &cloudwatchlogs.DescribeLogStreamsInput{
		LogGroupName: aws.String(name),
		Descending:   aws.Bool(true),
		OrderBy:      aws.String(cloudwatchlogs.OrderByLastEventTime),
	})
	if err != nil {
		return nil, err
	}
	return out.LogStreams, nil
}

func LogsStreams(ctx context.Context, name string) ([]*cloudwatchlogs.LogStream, error) {
	var res []*cloudwatchlogs.LogStream
	var token *string
	for {
		out, err := LogsClient().DescribeLogStreamsWithContext(ctx, &cloudwatchlogs.DescribeLogStreamsInput{
			LogGroupName: aws.String(name),
			Descending:   aws.Bool(true),
			OrderBy:      aws.String(cloudwatchlogs.OrderByLastEventTime),
			NextToken:    token,
		})
		if err != nil {
			return nil, err
		}
		res = append(res, out.LogStreams...)
		if out.NextToken == nil {
			break
		}
		token = out.NextToken
	}
	return res, nil
}

func LogsTail(ctx context.Context, name string, minAge time.Time, callback func(timestamp time.Time, line string)) error {
	tokens := make(map[string]*string)
	minAges := make(map[string]time.Time)
	for {
		streams, err := LogsMostRecentStreams(ctx, name)
		if err != nil {
			return err
		}
		data := false
		for _, stream := range streams {
			streamName := *stream.LogStreamName
			token := tokens[streamName]
			out, err := LogsClient().GetLogEventsWithContext(ctx, &cloudwatchlogs.GetLogEventsInput{
				LogGroupName:  aws.String(name),
				LogStreamName: aws.String(streamName),
				NextToken:     token,
			})
			if err != nil {
				return err
			}
			if len(out.Events) != 0 {
				tokens[streamName] = out.NextForwardToken
			}
			min, ok := minAges[streamName]
			if !ok {
				min = minAge
			}
			for _, log := range out.Events {
				ts := time.UnixMilli(*log.IngestionTime)
				if !ts.After(min) {
					continue
				}
				minAges[streamName] = ts
				data = true
				callback(time.UnixMilli(*log.Timestamp), strings.TrimRight(strings.ReplaceAll(*log.Message, "\t", " "), "\n"))
			}
		}
		if !data {
			time.Sleep(1 * time.Second)
		}
	}
}
