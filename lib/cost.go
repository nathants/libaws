package lib

import (
	"regexp"
	"strings"
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

var serviceNameDashes = regexp.MustCompile(`\-+`)

func NormalizeServiceName(name string) string {
	name = strings.ReplaceAll(name, "AWS ", "")
	name = strings.ReplaceAll(name, "Amazon Simple Storage Service", "S3")
	name = strings.ReplaceAll(name, "Amazon EC2 Container Registry (ECR)", "ECR")
	name = strings.ReplaceAll(name, "Amazon ", "")
	name = strings.ReplaceAll(name, "Simple Queue Service ", "SQS")
	name = strings.ReplaceAll(name, "API Gateway", "ApiGateway")
	name = strings.ReplaceAll(name, "AmazonCloudWatch", "Cloudwatch")
	name = strings.ReplaceAll(name, " ", "-")
	name = serviceNameDashes.ReplaceAllString(name, "-")
	name = strings.ReplaceAll(name, "Elastic-Compute-Cloud-Compute", "EC2")
	return name
}
