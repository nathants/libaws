package lib

import (
	"context"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/acm"
	acmtypes "github.com/aws/aws-sdk-go-v2/service/acm/types"
)

var acmClient *acm.Client
var acmClientLock sync.Mutex

func AcmClientExplicit(accessKeyID, accessKeySecret, region string) *acm.Client {
	return acm.NewFromConfig(*SessionExplicit(accessKeyID, accessKeySecret, region))
}

func AcmClient() *acm.Client {
	acmClientLock.Lock()
	defer acmClientLock.Unlock()
	if acmClient == nil {
		acmClient = acm.NewFromConfig(*Session())
	}
	return acmClient
}

func AcmListCertificates(ctx context.Context) ([]acmtypes.CertificateSummary, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "AcmListCertificates"}
		d.Start()
		defer d.End()
	}
	var token *string
	var result []acmtypes.CertificateSummary
	for {
		out, err := AcmClient().ListCertificates(ctx, &acm.ListCertificatesInput{
			NextToken: token,
		})
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		result = append(result, out.CertificateSummaryList...)
		if out.NextToken == nil {
			break
		}
		token = out.NextToken
	}
	return result, nil
}
