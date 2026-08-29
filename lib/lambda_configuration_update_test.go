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

type fakeLambdaConfigurationUpdateClient struct {
	calls       []string
	current     *lambda.GetFunctionConfigurationOutput
	updateInput *lambda.UpdateFunctionConfigurationInput
}

func (client *fakeLambdaConfigurationUpdateClient) GetFunctionConfiguration(
	_ context.Context,
	_ *lambda.GetFunctionConfigurationInput,
	_ ...func(*lambda.Options),
) (*lambda.GetFunctionConfigurationOutput, error) {
	client.calls = append(client.calls, "read")
	return client.current, nil
}

func (client *fakeLambdaConfigurationUpdateClient) UpdateFunctionConfiguration(
	_ context.Context,
	input *lambda.UpdateFunctionConfigurationInput,
	_ ...func(*lambda.Options),
) (*lambda.UpdateFunctionConfigurationOutput, error) {
	client.calls = append(client.calls, "update")
	client.updateInput = input
	return &lambda.UpdateFunctionConfigurationOutput{}, nil
}

func (client *fakeLambdaConfigurationUpdateClient) GetFunction(
	_ context.Context,
	_ *lambda.GetFunctionInput,
	_ ...func(*lambda.Options),
) (*lambda.GetFunctionOutput, error) {
	client.calls = append(client.calls, "wait")
	return &lambda.GetFunctionOutput{
		Configuration: &lambdatypes.FunctionConfiguration{
			LastUpdateStatus: lambdatypes.LastUpdateStatusSuccessful,
		},
	}, nil
}

func TestLambdaEnsureFunctionConfigurationWaitsForCompletedUpdate(t *testing.T) {
	client := &fakeLambdaConfigurationUpdateClient{
		current: &lambda.GetFunctionConfigurationOutput{
			Environment: &lambdatypes.EnvironmentResponse{
				Variables: map[string]string{"MODE": "old"},
			},
			Timeout:    aws.Int32(30),
			MemorySize: aws.Int32(128),
		},
	}

	err := lambdaEnsureFunctionConfigurationWithClient(
		context.Background(),
		client,
		&InfraLambda{Name: "function"},
		map[string]string{"MODE": "new"},
		30,
		128,
		false,
		false,
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(client.calls, []string{"read", "update", "wait"}) {
		t.Fatalf("Lambda configuration calls=%v, want read, update, wait", client.calls)
	}
	if got := client.updateInput.Environment.Variables["MODE"]; got != "new" {
		t.Fatalf("updated MODE=%q, want new", got)
	}
}

func TestLambdaEnsureFunctionConfigurationDoesNotWaitWithoutUpdate(t *testing.T) {
	client := &fakeLambdaConfigurationUpdateClient{
		current: &lambda.GetFunctionConfigurationOutput{
			Environment: &lambdatypes.EnvironmentResponse{
				Variables: map[string]string{"MODE": "current"},
			},
			Timeout:    aws.Int32(30),
			MemorySize: aws.Int32(128),
		},
	}

	err := lambdaEnsureFunctionConfigurationWithClient(
		context.Background(),
		client,
		&InfraLambda{Name: "function"},
		map[string]string{"MODE": "current"},
		30,
		128,
		false,
		false,
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(client.calls, []string{"read"}) {
		t.Fatalf("unchanged Lambda configuration calls=%v, want read only", client.calls)
	}
}

func TestLambdaEnsureFunctionConfigurationPreservesExplicitEmptyValue(t *testing.T) {
	client := &fakeLambdaConfigurationUpdateClient{
		current: &lambda.GetFunctionConfigurationOutput{
			Environment: &lambdatypes.EnvironmentResponse{Variables: map[string]string{}},
			Timeout:     aws.Int32(30),
			MemorySize:  aws.Int32(128),
		},
	}

	err := lambdaEnsureFunctionConfigurationWithClient(
		context.Background(),
		client,
		&InfraLambda{Name: "function"},
		map[string]string{"OPTIONAL": ""},
		30,
		128,
		false,
		false,
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(client.calls, []string{"read", "update", "wait"}) {
		t.Fatalf("Lambda configuration calls=%v, want read, update, wait", client.calls)
	}
	value, exists := client.updateInput.Environment.Variables["OPTIONAL"]
	if !exists || value != "" {
		t.Fatalf("explicit empty Lambda environment value missing from update: %#v", client.updateInput.Environment.Variables)
	}
}
