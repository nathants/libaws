package lib

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

type fakeLambdaCreateFunctionClient struct {
	calls []string
	final *lambda.GetFunctionOutput
}

func (client *fakeLambdaCreateFunctionClient) CreateFunction(
	_ context.Context,
	_ *lambda.CreateFunctionInput,
	_ ...func(*lambda.Options),
) (*lambda.CreateFunctionOutput, error) {
	client.calls = append(client.calls, "create")
	return &lambda.CreateFunctionOutput{}, nil
}

func (client *fakeLambdaCreateFunctionClient) GetFunction(
	_ context.Context,
	_ *lambda.GetFunctionInput,
	_ ...func(*lambda.Options),
) (*lambda.GetFunctionOutput, error) {
	client.calls = append(client.calls, "wait")
	return client.final, nil
}

func TestCreateLambdaFunctionWaitsForExactPublishedZip(t *testing.T) {
	zipBytes := []byte("exact created Lambda deployment zip")
	client := &fakeLambdaCreateFunctionClient{
		final: &lambda.GetFunctionOutput{
			Configuration: &lambdatypes.FunctionConfiguration{
				CodeSha256:  aws.String(lambdaZipHashForTest(zipBytes)),
				FunctionArn: aws.String("arn:aws:lambda:us-east-1:012345678901:function:function"),
				State:       lambdatypes.StateActive,
			},
		},
	}

	out, err := createLambdaFunction(
		context.Background(),
		client,
		&lambda.CreateFunctionInput{
			FunctionName: aws.String("function"),
			Code:         &lambdatypes.FunctionCode{ZipFile: zipBytes},
		},
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	if out != client.final {
		t.Fatal("Lambda create did not return the exact waiter output")
	}
	if !slices.Equal(client.calls, []string{"create", "wait"}) {
		t.Fatalf("Lambda create calls=%v, want create, wait", client.calls)
	}
}
