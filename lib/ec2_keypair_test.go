package lib

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

type ec2KeypairTestClient struct {
	code, message string
	deleted       bool
}

func (client *ec2KeypairTestClient) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	body, status := "", http.StatusOK
	switch r.Form.Get("Action") {
	case "DescribeKeyPairs":
		if client.code != "" {
			body = fmt.Sprintf(`<Response><Errors><Error><Code>%s</Code><Message>%s</Message></Error></Errors></Response>`, client.code, client.message)
			status = http.StatusBadRequest
		} else {
			body = `<DescribeKeyPairsResponse><keySet><item><keyName>test-keypair</keyName></item></keySet></DescribeKeyPairsResponse>`
		}
	case "DeleteKeyPair":
		client.deleted = true
		body = `<DeleteKeyPairResponse><return>true</return></DeleteKeyPairResponse>`
	default:
		return nil, fmt.Errorf("unexpected action: %v", r.Form)
	}
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}

func TestEC2DeleteKeypair(t *testing.T) {
	for _, preview := range []bool{true, false} {
		for _, code := range []string{"InvalidKeyPair.NotFound", "UnauthorizedOperation", ""} {
			t.Run(fmt.Sprintf("preview=%t/code=%s", preview, code), func(t *testing.T) {
				client := &ec2KeypairTestClient{code: code, message: "InvalidKeyPair.NotFound in a message is not proof of absence"}
				oldClient := ec2Client
				ec2Client = ec2.NewFromConfig(aws.Config{
					Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
					HTTPClient: &http.Client{Transport: client}, RetryMaxAttempts: 1,
				})
				t.Cleanup(func() { ec2Client = oldClient })
				err := EC2DeleteKeypair(context.Background(), "test-keypair", preview)
				if code == "UnauthorizedOperation" {
					if err == nil || !strings.Contains(err.Error(), code) {
						t.Fatalf("AWS error lost: %v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if client.deleted != (code == "" && !preview) {
					t.Fatalf("unexpected mutation: deleted=%t preview=%t code=%s", client.deleted, preview, code)
				}
			})
		}
	}
}
