package lib

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/service/sts"
)

var stsClient *sts.STS
var stsClientLock sync.RWMutex

func STSClientExplicit(accessKeyID, accessKeySecret, region string) *sts.STS {
	return sts.New(SessionExplicit(accessKeyID, accessKeySecret, region))
}

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
		if doDebug {
			d := &Debug{start: time.Now(), name: "StsAccount"}
			defer d.Log()
		}
		out, err := STSClient().GetCallerIdentityWithContext(ctx, &sts.GetCallerIdentityInput{})
		if err != nil {
			return "", err
		}
		stsAccount = out.Account
	}
	return *stsAccount, nil
}

var stsArn *string
var stsArnLock sync.RWMutex

func StsArn(ctx context.Context) (string, error) {
	stsArnLock.Lock()
	defer stsArnLock.Unlock()
	if stsArn == nil {
		if doDebug {
			d := &Debug{start: time.Now(), name: "StsArn"}
			defer d.Log()
		}
		out, err := STSClient().GetCallerIdentityWithContext(ctx, &sts.GetCallerIdentityInput{})
		if err != nil {
			return "", err
		}
		stsArn = out.Arn
	}
	return *stsArn, nil
}

var stsUser *string
var stsUserLock sync.RWMutex

func StsUser(ctx context.Context) (string, error) {
	stsUserLock.Lock()
	defer stsUserLock.Unlock()
	if stsUser == nil {
		if doDebug {
			d := &Debug{start: time.Now(), name: "StsUser"}
			defer d.Log()
		}
		out, err := STSClient().GetCallerIdentityWithContext(ctx, &sts.GetCallerIdentityInput{})
		if err != nil {
			return "", err
		}
		user := Last(strings.Split(*out.Arn, ":"))
		stsUser = &user
	}
	return *stsUser, nil
}
