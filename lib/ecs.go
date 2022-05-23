package lib

import (
	"sync"

	"github.com/aws/aws-sdk-go/service/ecs"
)

var ecsClient *ecs.ECS
var ecsClientLock sync.RWMutex

func ECSClientExplicit(accessKeyID, accessKeySecret, region string) *ecs.ECS {
	return ecs.New(SessionExplicit(accessKeyID, accessKeySecret, region))
}

func ECSClient() *ecs.ECS {
	ecsClientLock.Lock()
	defer ecsClientLock.Unlock()
	if ecsClient == nil {
		ecsClient = ecs.New(Session())
	}
	return ecsClient
}
