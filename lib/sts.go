package lib

import (
	"context"
	"sync"

	"github.com/aws/aws-sdk-go/service/sts"
)

var stsClient *sts.STS
var stsClientLock sync.RWMutex

func STSClient() *sts.STS {
	stsClientLock.Lock()
	defer stsClientLock.Unlock()
	if stsClient == nil {
		stsClient = sts.New(Session())
	}
	return stsClient
}

var account *string
var accountLock sync.RWMutex

func Account(ctx context.Context) (string, error) {
	accountLock.Lock()
	defer accountLock.Unlock()
	if account == nil {
		out, err := STSClient().GetCallerIdentityWithContext(ctx, &sts.GetCallerIdentityInput{})
		if err != nil {
			return "", err
		}
		account = out.Account
	}
	return *account, nil
}
