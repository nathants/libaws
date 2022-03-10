package lib

import (
	"sync"

	// "github.com/aws/aws-sdk-go/aws"
	// "github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ecs"
)

var ecsClient *ecs.ECS
var ecsClientLock sync.RWMutex

func ECSClientClear() {
	ecsClientLock.Lock()
	defer ecsClientLock.Unlock()
	ecsClient = nil
	sessionClear()
}

func ECSClient() *ecs.ECS {
	ecsClientLock.Lock()
	defer ecsClientLock.Unlock()
	if ecsClient == nil {
		ecsClient = ecs.New(Session())
	}
	return ecsClient
}
