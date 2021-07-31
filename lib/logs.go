package lib

import (
	"sync"

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
