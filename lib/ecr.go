package lib

import (
	"github.com/aws/aws-sdk-go/service/ecr"
	"sync"
)

var ecrClient *ecr.ECR
var ecrClientLock sync.RWMutex

func EcrClient() *ecr.ECR {
	ecrClientLock.Lock()
	defer ecrClientLock.Unlock()
	if ecrClient == nil {
		ecrClient = ecr.New(Session())
	}
	return ecrClient
}
