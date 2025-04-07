package lib

import (
	"sync"

	"github.com/aws/aws-sdk-go-v2/service/ecs"
)

var ecsClient *ecs.Client
var ecsClientLock sync.Mutex

func ECSClientExplicit(accessKeyID, accessKeySecret, region string) *ecs.Client {
	return ecs.NewFromConfig(*SessionExplicit(accessKeyID, accessKeySecret, region))
}

func ECSClient() *ecs.Client {
	ecsClientLock.Lock()
	defer ecsClientLock.Unlock()
	if ecsClient == nil {
		ecsClient = ecs.NewFromConfig(*Session())
	}
	return ecsClient
}
