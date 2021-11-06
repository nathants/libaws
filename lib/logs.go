package lib

import (
	"context"
	"sync"

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
	_, err := LogsClient().DeleteLogGroupWithContext(ctx, &cloudwatchlogs.DeleteLogGroupInput{
		LogGroupName: aws.String(name),
	})
	aerr, ok := err.(awserr.Error)
	if !ok || aerr.Code() != "ResourceNotFoundException" {
		Logger.Println("error:", err)
		return err
	}
	return nil
}
