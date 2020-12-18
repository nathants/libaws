package lib

import (
	"sync"
	"github.com/aws/aws-sdk-go/service/iam"
)

var iamClient *iam.IAM
var iamClientLock sync.RWMutex

func IAMClient() *iam.IAM {
	iamClientLock.Lock()
	defer iamClientLock.Unlock()
	if iamClient == nil {
		iamClient = iam.New(Session())
	}
	return iamClient
}
