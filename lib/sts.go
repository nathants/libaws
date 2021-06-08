package lib

import (
	"context"
	"strings"
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

var stsAccount *string
var stsAccountLock sync.RWMutex

func StsAccount(ctx context.Context) (string, error) {
	stsAccountLock.Lock()
	defer stsAccountLock.Unlock()
	if stsAccount == nil {
		out, err := STSClient().GetCallerIdentityWithContext(ctx, &sts.GetCallerIdentityInput{})
		if err != nil {
			return "", err
		}
		stsAccount = out.Account
	}
	return *stsAccount, nil
}

func StsUser(ctx context.Context) (string, error) {
	out, err := STSClient().GetCallerIdentityWithContext(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", err
	}
	return strings.SplitN(*out.Arn, "/", 2)[1], nil
}
