package lib

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	orgtypes "github.com/aws/aws-sdk-go-v2/service/organizations/types"
)

var organizationsClient *organizations.Client
var organizationsClientLock sync.Mutex

func OrganizationsClientExplicit(accessKeyID, accessKeySecret, region string) *organizations.Client {
	return organizations.NewFromConfig(*SessionExplicit(accessKeyID, accessKeySecret, region))
}

func OrganizationsClient() *organizations.Client {
	organizationsClientLock.Lock()
	defer organizationsClientLock.Unlock()
	if organizationsClient == nil {
		organizationsClient = organizations.NewFromConfig(*Session())
	}
	return organizationsClient
}

func OrganizationsEnsure(ctx context.Context, name, email string, preview bool) (string, error) {
	if doDebug {
		d := &Debug{start: time.Now(), name: "OrganizationsEnsure"}
		defer d.Log()
	}
	var token *string
	var accounts []orgtypes.Account
	for {
		out, err := OrganizationsClient().ListAccounts(ctx, &organizations.ListAccountsInput{
			NextToken: token,
		})
		if err != nil {
			Logger.Println("error:", err)
			return "", err
		}
		accounts = append(accounts, out.Accounts...)
		if out.NextToken == nil {
			break
		}
		token = out.NextToken
	}
	var account orgtypes.Account
	count := 0
	for _, a := range accounts {
		if email == *a.Email && name == *a.Name {
			account = a
			count++
		}
	}
	switch count {
	case 0:
	case 1:
		return *account.Id, nil
	default:
		err := fmt.Errorf("found more than 1 (%d) account for: %s %s", count, name, email)
		Logger.Println("error:", err)
		return "", nil
	}
	Logger.Println(PreviewString(preview)+"organizations created account:", name, email)
	if !preview {
		createOut, err := OrganizationsClient().CreateAccount(ctx, &organizations.CreateAccountInput{
			AccountName: aws.String(name),
			Email:       aws.String(email),
		})
		if err != nil {
			Logger.Println("error:", err)
			return "", err
		}
		requestID := createOut.CreateAccountStatus.Id
		for {
			describeOut, err := OrganizationsClient().DescribeCreateAccountStatus(ctx, &organizations.DescribeCreateAccountStatusInput{
				CreateAccountRequestId: requestID,
			})
			if err != nil {
				Logger.Println("error:", err)
				return "", err
			}
			switch describeOut.CreateAccountStatus.State {
			case orgtypes.CreateAccountStateSucceeded:
				return *describeOut.CreateAccountStatus.AccountId, nil
			case orgtypes.CreateAccountStateFailed:
				err := fmt.Errorf("account creation failed: %s", describeOut.CreateAccountStatus.FailureReason)
				Logger.Println("error:", err)
				return "", err
			case orgtypes.CreateAccountStateInProgress:
				time.Sleep(5 * time.Second)
			default:
				err := fmt.Errorf("unknown state: %s", describeOut.CreateAccountStatus.State)
				Logger.Println("error:", err)
				return "", err
			}
		}
	}
	return "", nil
}
