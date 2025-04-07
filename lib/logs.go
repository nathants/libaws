package lib

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cwlogstypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

var logsClient *cloudwatchlogs.Client
var logsClientLock sync.Mutex

func LogsClientExplicit(accessKeyID, accessKeySecret, region string) *cloudwatchlogs.Client {
	return cloudwatchlogs.NewFromConfig(*SessionExplicit(accessKeyID, accessKeySecret, region))
}

func LogsClient() *cloudwatchlogs.Client {
	logsClientLock.Lock()
	defer logsClientLock.Unlock()
	if logsClient == nil {
		logsClient = cloudwatchlogs.NewFromConfig(*Session())
	}
	return logsClient
}

func LogsEnsureGroup(ctx context.Context, infrasetName, logGroupName string, ttlDays int, preview bool) error {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LogsEnsureGroup"}
		defer d.Log()
	}
	out, err := LogsClient().DescribeLogStreams(ctx, &cloudwatchlogs.DescribeLogStreamsInput{
		LogGroupName: aws.String(logGroupName),
	})
	if err != nil || out.LogStreams == nil {
		if err != nil {
			var rnfe *cwlogstypes.ResourceNotFoundException
			if !errors.As(err, &rnfe) {
				Logger.Println("error:", err)
				return err
			}
		}
		if !preview {
			_, err := LogsClient().CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{
				LogGroupName: aws.String(logGroupName),
				Tags: map[string]string{
					infraSetTagName: infrasetName,
				},
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Println(PreviewString(preview)+"created log group:", logGroupName)
	}
	outGroups, err := LogsClient().DescribeLogGroups(ctx, &cloudwatchlogs.DescribeLogGroupsInput{
		LogGroupNamePrefix: aws.String(logGroupName),
	})
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	var logGroup *cwlogstypes.LogGroup
	for i, lg := range outGroups.LogGroups {
		if logGroupName == *lg.LogGroupName {
			logGroup = &outGroups.LogGroups[i]
			break
		}
	}
	if logGroup == nil && !preview {
		err := fmt.Errorf("expected exactly 1 logGroup with name: %s", logGroupName)
		Logger.Println("error:", err)
		return err
	}
	if logGroup == nil {
		logGroup = &cwlogstypes.LogGroup{}
	}
	if logGroup.RetentionInDays == nil {
		logGroup.RetentionInDays = aws.Int32(0)
	}
	if ttlDays != int(*logGroup.RetentionInDays) {
		if !preview {
			_, err = LogsClient().PutRetentionPolicy(ctx, &cloudwatchlogs.PutRetentionPolicyInput{
				LogGroupName:    aws.String(logGroupName),
				RetentionInDays: aws.Int32(int32(ttlDays)),
			})
			if err != nil {
				Logger.Println("error:", err)
				return err
			}
		}
		Logger.Printf(PreviewString(preview)+"updated log ttl days for %s: %d => %d\n", logGroupName, *logGroup.RetentionInDays, ttlDays)
	}
	return nil
}

func LogsListLogGroups(ctx context.Context) ([]cwlogstypes.LogGroup, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LogsListLogGroups"}
		defer d.Log()
	}
	var token *string
	var logs []cwlogstypes.LogGroup
	for {
		out, err := LogsClient().DescribeLogGroups(ctx, &cloudwatchlogs.DescribeLogGroupsInput{
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
	if doDebug {
		d := &Debug{start: time.Now(), name: "LogsDeleteGroup"}
		defer d.Log()
	}
	_, err := LogsClient().DescribeLogStreams(ctx, &cloudwatchlogs.DescribeLogStreamsInput{
		LogGroupName: aws.String(name),
	})
	if err != nil {
		var rnfe *cwlogstypes.ResourceNotFoundException
		if !errors.As(err, &rnfe) {
			Logger.Println("error:", err)
			return err
		}
		return nil
	}
	if !preview {
		_, err := LogsClient().DeleteLogGroup(ctx, &cloudwatchlogs.DeleteLogGroupInput{
			LogGroupName: aws.String(name),
		})
		if err != nil {
			var rnfe *cwlogstypes.ResourceNotFoundException
			if !errors.As(err, &rnfe) {
				Logger.Println("error:", err)
				return err
			}
		}
	}
	Logger.Println(PreviewString(preview)+"deleted log group:", name)
	return nil
}

func LogsMostRecentStreams(ctx context.Context, name string) ([]cwlogstypes.LogStream, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LogsMostRecentStreams"}
		defer d.Log()
	}
	out, err := LogsClient().DescribeLogStreams(ctx, &cloudwatchlogs.DescribeLogStreamsInput{
		LogGroupName: aws.String(name),
		Descending:   aws.Bool(true),
		OrderBy:      cwlogstypes.OrderByLastEventTime,
	})
	if err != nil {
		return nil, err
	}
	return out.LogStreams, nil
}

func LogsStreams(ctx context.Context, name string) ([]cwlogstypes.LogStream, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "LogStreams"}
		defer d.Log()
	}
	var res []cwlogstypes.LogStream
	var token *string
	for {
		out, err := LogsClient().DescribeLogStreams(ctx, &cloudwatchlogs.DescribeLogStreamsInput{
			LogGroupName: aws.String(name),
			Descending:   aws.Bool(true),
			OrderBy:      cwlogstypes.OrderByLastEventTime,
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
	if doDebug {
		d := &Debug{start: time.Now(), name: "LogsTail"}
		defer d.Log()
	}
	tokens := make(map[string]*string)
	for {
		streams, err := LogsMostRecentStreams(ctx, name)
		if err != nil {
			return err
		}
		data := false
		for _, stream := range streams {
			streamName := *stream.LogStreamName
			token := tokens[streamName]
			out, err := LogsClient().GetLogEvents(ctx, &cloudwatchlogs.GetLogEventsInput{
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
			for _, log := range out.Events {
				ts := FromUnixMilli(*log.IngestionTime)
				if !ts.After(minAge) {
					continue
				}
				data = true
				callback(FromUnixMilli(*log.Timestamp), strings.TrimRight(strings.ReplaceAll(*log.Message, "\t", " "), "\n"))
			}
		}
		if !data {
			time.Sleep(1 * time.Second)
		}
	}
}
