package lib

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

func TestIamEnsurePoliciesWithEmptyDesiredSetsSkipAccountPolicyInventory(t *testing.T) {
	var lock sync.Mutex
	var actions []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		action := values.Get("Action")
		lock.Lock()
		actions = append(actions, action)
		lock.Unlock()
		writer.Header().Set("Content-Type", "text/xml")
		var response string
		switch action {
		case "ListAttachedRolePolicies":
			response = `<ListAttachedRolePoliciesResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/"><ListAttachedRolePoliciesResult><IsTruncated>false</IsTruncated></ListAttachedRolePoliciesResult><ResponseMetadata><RequestId>test</RequestId></ResponseMetadata></ListAttachedRolePoliciesResponse>`
		case "ListAttachedUserPolicies":
			response = `<ListAttachedUserPoliciesResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/"><ListAttachedUserPoliciesResult><IsTruncated>false</IsTruncated></ListAttachedUserPoliciesResult><ResponseMetadata><RequestId>test</RequestId></ResponseMetadata></ListAttachedUserPoliciesResponse>`
		default:
			http.Error(writer, "unexpected IAM action: "+action, http.StatusInternalServerError)
			return
		}
		if _, err := fmt.Fprint(writer, response); err != nil {
			t.Errorf("write IAM response: %v", err)
		}
	}))
	defer server.Close()

	client := iam.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider("test-access", "test-secret", "")),
		HTTPClient:  server.Client(),
	}, func(options *iam.Options) {
		options.BaseEndpoint = aws.String(server.URL)
	})
	iamClientLock.Lock()
	previousClient := iamClient
	iamClient = client
	iamClientLock.Unlock()
	t.Cleanup(func() {
		iamClientLock.Lock()
		iamClient = previousClient
		iamClientLock.Unlock()
	})

	if err := IamEnsureRolePolicies(context.Background(), "role", nil, false); err != nil {
		t.Fatal(err)
	}
	if err := IamEnsureUserPolicies(context.Background(), "user", nil, false); err != nil {
		t.Fatal(err)
	}
	lock.Lock()
	defer lock.Unlock()
	want := []string{"ListAttachedRolePolicies", "ListAttachedUserPolicies", "ListAttachedUserPolicies"}
	if !slices.Equal(actions, want) {
		t.Fatalf("IAM actions = %v, want %v", actions, want)
	}
}
