package lib

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

type fakeLambdaCodeUpdateClient struct {
	updateHash string
	finalHash  string
	calls      []string
}

func (client *fakeLambdaCodeUpdateClient) UpdateFunctionCode(
	_ context.Context,
	_ *lambda.UpdateFunctionCodeInput,
	_ ...func(*lambda.Options),
) (*lambda.UpdateFunctionCodeOutput, error) {
	client.calls = append(client.calls, "update")
	return &lambda.UpdateFunctionCodeOutput{CodeSha256: aws.String(client.updateHash)}, nil
}

func (client *fakeLambdaCodeUpdateClient) GetFunction(
	_ context.Context,
	_ *lambda.GetFunctionInput,
	_ ...func(*lambda.Options),
) (*lambda.GetFunctionOutput, error) {
	client.calls = append(client.calls, "get")
	return &lambda.GetFunctionOutput{
		Configuration: &lambdatypes.FunctionConfiguration{
			CodeSha256:       aws.String(client.finalHash),
			LastUpdateStatus: lambdatypes.LastUpdateStatusSuccessful,
		},
	}, nil
}

func lambdaZipHashForTest(data []byte) string {
	hash := sha256.Sum256(data)
	return base64.StdEncoding.EncodeToString(hash[:])
}

func TestUpdateLambdaFunctionCodeWaitsForExactPublishedZip(t *testing.T) {
	zipBytes := []byte("exact lambda deployment zip")
	expectedHash := lambdaZipHashForTest(zipBytes)
	client := &fakeLambdaCodeUpdateClient{updateHash: expectedHash, finalHash: expectedHash}

	if err := updateLambdaFunctionCode(
		context.Background(), client, "better-beta", zipBytes, "", time.Second,
	); err != nil {
		t.Fatal(err)
	}
	if strings.Join(client.calls, ",") != "update,get" {
		t.Fatalf("expected update followed by waiter read, got %#v", client.calls)
	}
}

func TestUpdateLambdaFunctionCodeRejectsUnexpectedPublishedZip(t *testing.T) {
	zipBytes := []byte("exact lambda deployment zip")
	expectedHash := lambdaZipHashForTest(zipBytes)
	client := &fakeLambdaCodeUpdateClient{updateHash: expectedHash, finalHash: "old-code-hash"}

	err := updateLambdaFunctionCode(
		context.Background(), client, "better-beta", zipBytes, "", time.Second,
	)
	if err == nil || !strings.Contains(err.Error(), "code hash") {
		t.Fatalf("expected a code-hash mismatch, got %v", err)
	}
}
