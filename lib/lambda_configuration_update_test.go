package lib

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strings"
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

// Exercise libaws's update path through the real SDK serializer without AWS.
type lambdaConfigurationHTTPForTest struct {
	requests    int
	updateBytes int
}

func (client *lambdaConfigurationHTTPForTest) Do(request *http.Request) (*http.Response, error) {
	client.requests++
	response := `{"Configuration":{"LastUpdateStatus":"Successful"}}`
	if strings.HasSuffix(request.URL.Path, "/configuration") {
		response = `{"Timeout":1,"MemorySize":128}`
		if request.Method == http.MethodPut {
			body, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			client.updateBytes = len(body)
			response = `{}`
		}
	}
	return &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{},
		Body: io.NopCloser(strings.NewReader(response)),
	}, nil
}

func TestLambdaConfigurationRequestBoundaryMatchesSDK(t *testing.T) {
	for _, test := range []struct {
		name, character          string
		padding, timeout, memory int
		emptyVariable            bool
	}{
		{"backspace", "\b", 251, 60, 128, false},
		{"form feed", "\f", 251, 60, 128, false},
		{"line separator", "\u2028", 251, 60, 128, false},
		{"paragraph separator", "\u2029", 251, 60, 128, false},
		{"attributes", "\b", 248, 900, 10240, false},
		{"multiple variables", "\b", 243, 60, 128, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := &lambdaConfigurationHTTPForTest{}
			client := lambda.New(lambda.Options{
				Region: "us-east-1", Credentials: aws.AnonymousCredentials{}, HTTPClient: transport,
			})
			variables := map[string]string{"AA": strings.Repeat(test.character, 800) + strings.Repeat("x", test.padding)}
			if test.emptyVariable {
				variables["BB"] = ""
			}
			err := lambdaEnsureFunctionConfigurationWithClient(
				context.Background(), client, &InfraLambda{Name: "function"}, variables,
				test.timeout, test.memory, false, false, time.Second,
			)
			if err != nil {
				t.Fatal(err)
			}
			if transport.updateBytes != 5120 || transport.requests != 3 {
				t.Fatalf("SDK update bytes=%d requests=%d, want 5120 bytes and read/update/wait", transport.updateBytes, transport.requests)
			}
			variables["AA"] += "x"
			for _, preview := range []bool{false, true} {
				transport.requests = 0
				err = lambdaEnsureFunctionConfigurationWithClient(
					context.Background(), client, &InfraLambda{Name: "function"}, variables,
					test.timeout, test.memory, preview, false, time.Second,
				)
				if err == nil || !strings.Contains(err.Error(), "request size 5121 bytes") || transport.requests != 0 {
					t.Fatalf("oversized update preview=%v error=%v requests=%d, want local rejection before AWS", preview, err, transport.requests)
				}
			}
		})
	}
}
