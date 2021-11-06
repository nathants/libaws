package lib

import (
	"context"
	"sync"

	"github.com/aws/aws-sdk-go/service/acm"
)

var acmClient *acm.ACM
var acmClientLock sync.RWMutex

func AcmClient() *acm.ACM {
	acmClientLock.Lock()
	defer acmClientLock.Unlock()
	if acmClient == nil {
		acmClient = acm.New(Session())
	}
	return acmClient
}

func AcmListCertificates(ctx context.Context) ([]*acm.CertificateSummary, error) {
	var token *string
	var result []*acm.CertificateSummary
	for {
		out, err := AcmClient().ListCertificatesWithContext(ctx, &acm.ListCertificatesInput{
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
