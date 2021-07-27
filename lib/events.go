package lib

import (
	"sync"

	"github.com/aws/aws-sdk-go/service/cloudwatchevents"
)

var eventsClient *cloudwatchevents.CloudWatchEvents
var eventsClientLock sync.RWMutex

func EventsClient() *cloudwatchevents.CloudWatchEvents {
	eventsClientLock.Lock()
	defer eventsClientLock.Unlock()
	if eventsClient == nil {
		eventsClient = cloudwatchevents.New(Session())
	}
	return eventsClient
}
