package lib

import (
	"sync"

	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
)

var costExplorerClient *costexplorer.Client
var costExplorerClientLock sync.Mutex

func CostExplorerClientExplicit(accessKeyID, accessKeySecret, region string) *costexplorer.Client {
	return costexplorer.NewFromConfig(*SessionExplicit(accessKeyID, accessKeySecret, region))
}

func CostExplorerClient() *costexplorer.Client {
	costExplorerClientLock.Lock()
	defer costExplorerClientLock.Unlock()
	if costExplorerClient == nil {
		costExplorerClient = costexplorer.NewFromConfig(*Session())
	}
	return costExplorerClient
}
