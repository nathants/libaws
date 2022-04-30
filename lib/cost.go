package lib

import (
	"sync"

	"github.com/aws/aws-sdk-go/service/costexplorer"
)

var costExplorerClient *costexplorer.CostExplorer
var costExplorerClientLock sync.RWMutex

func CostExplorerClientExplicit(accessKeyID, accessKeySecret, region string) *costexplorer.CostExplorer {
	return costexplorer.New(SessionExplicit(accessKeyID, accessKeySecret, region))
}

func CostExplorerClient() *costexplorer.CostExplorer {
	costExplorerClientLock.Lock()
	defer costExplorerClientLock.Unlock()
	if costExplorerClient == nil {
		costExplorerClient = costexplorer.New(Session())
	}
	return costExplorerClient
}
