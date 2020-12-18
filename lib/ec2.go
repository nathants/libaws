package lib

import (
	"sync"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

var ec2Client *ec2.Client
var ec2ClientLock sync.RWMutex

func EC2Client() *ec2.Client {
	ec2ClientLock.Lock()
	defer ec2ClientLock.Unlock()
	if ec2Client == nil {
		ec2Client = ec2.NewFromConfig(Config())
	}
	return ec2Client
}
