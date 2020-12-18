package lib

import (
	"sync"

	"github.com/aws/aws-sdk-go-v2/service/iam"
)

var iamClient *iam.Client
var iamClientLock sync.RWMutex

func IAMClient() *iam.Client {
	iamClientLock.Lock()
	defer iamClientLock.Unlock()
	if iamClient == nil {
		iamClient = iam.NewFromConfig(Config())
	}
	return iamClient
}
