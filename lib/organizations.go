package lib

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/organizations"
)

var organizationsClient *organizations.Organizations
var organizationsClientLock sync.RWMutex

func OrganizationsClientClear() {
	organizationsClientLock.Lock()
	defer organizationsClientLock.Unlock()
	organizationsClient = nil
	sessionClear()
}

func OrganizationsClient() *organizations.Organizations {
	organizationsClientLock.Lock()
	defer organizationsClientLock.Unlock()
	if organizationsClient == nil {
		organizationsClient = organizations.New(Session())
	}
	return organizationsClient
}

func OrganizationsEnsure(ctx context.Context, name, email string, preview bool) (string, error) {
	//
	var token *string
	var accounts []*organizations.Account
	for {
		out, err := OrganizationsClient().ListAccountsWithContext(ctx, &organizations.ListAccountsInput{
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
	//
	var account organizations.Account
	count := 0
	for _, a := range accounts {
		if email == *a.Email && name == *a.Name {
			account = *a
			count++
		}
	}
	//
	switch count {
	case 0:
	case 1:
		return *account.Id, nil
	default:
		err := fmt.Errorf("found more than 1 (%d) account for: %s %s", count, name, email)
		Logger.Println("error:", err)
		return "", nil
	}
	//
	Logger.Println(PreviewString(preview)+"organizations created account:", name, email)
	if !preview {
		createOut, err := OrganizationsClient().CreateAccountWithContext(ctx, &organizations.CreateAccountInput{
			AccountName: aws.String(name),
			Email:       aws.String(email),
		})
		if err != nil {
			Logger.Println("error:", err)
			return "", err
		}
		requestID := createOut.CreateAccountStatus.Id
		for {
			describeOut, err := OrganizationsClient().DescribeCreateAccountStatusWithContext(ctx, &organizations.DescribeCreateAccountStatusInput{
				CreateAccountRequestId: requestID,
			})
			if err != nil {
				Logger.Println("error:", err)
				return "", err
			}
			switch *describeOut.CreateAccountStatus.State {
			case organizations.CreateAccountStateSucceeded:
				return *describeOut.CreateAccountStatus.AccountId, nil
			case organizations.CreateAccountStateFailed:
				err := fmt.Errorf("account creation failed: %s", *describeOut.CreateAccountStatus.FailureReason)
				Logger.Println("error:", err)
				return "", err
			case organizations.CreateAccountStateInProgress:
				time.Sleep(5 * time.Second)
			default:
				err := fmt.Errorf("unknown state: %s", *describeOut.CreateAccountStatus.State)
				Logger.Println("error:", err)
				return "", err
			}
		}
	}
	return "", nil
}
