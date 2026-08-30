package lib

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

type fakeLambdaConcurrencyClient struct {
	current      *int32
	getErr       error
	nilGetOutput bool
	getCalls     int
	putCalls     []int32
	deleteCalls  int
}

func cloneConcurrency(value *int32) *int32 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func (client *fakeLambdaConcurrencyClient) GetFunctionConcurrency(
	_ context.Context,
	_ *lambda.GetFunctionConcurrencyInput,
	_ ...func(*lambda.Options),
) (*lambda.GetFunctionConcurrencyOutput, error) {
	client.getCalls++
	if client.getErr != nil {
		return nil, client.getErr
	}
	if client.nilGetOutput {
		return nil, nil
	}
	return &lambda.GetFunctionConcurrencyOutput{
		ReservedConcurrentExecutions: cloneConcurrency(client.current),
	}, nil
}

func (client *fakeLambdaConcurrencyClient) PutFunctionConcurrency(
	_ context.Context,
	input *lambda.PutFunctionConcurrencyInput,
	_ ...func(*lambda.Options),
) (*lambda.PutFunctionConcurrencyOutput, error) {
	if input.ReservedConcurrentExecutions == nil {
		return nil, errors.New("test received nil reserved concurrency")
	}
	client.putCalls = append(client.putCalls, *input.ReservedConcurrentExecutions)
	client.current = cloneConcurrency(input.ReservedConcurrentExecutions)
	return &lambda.PutFunctionConcurrencyOutput{}, nil
}

func (client *fakeLambdaConcurrencyClient) DeleteFunctionConcurrency(
	_ context.Context,
	_ *lambda.DeleteFunctionConcurrencyInput,
	_ ...func(*lambda.Options),
) (*lambda.DeleteFunctionConcurrencyOutput, error) {
	client.deleteCalls++
	client.current = nil
	return &lambda.DeleteFunctionConcurrencyOutput{}, nil
}

func TestLambdaSetConcurrencyReconcilesOmittedReservedZeroAndPositive(t *testing.T) {
	for _, test := range []struct {
		name    string
		desired int32
	}{
		{name: "reserved zero", desired: 0},
		{name: "positive reservation", desired: 5},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeLambdaConcurrencyClient{}
			desired := test.desired

			if err := lambdaSetConcurrency(context.Background(), client, "function", &desired, false); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(client.putCalls, []int32{desired}) || client.deleteCalls != 0 {
				t.Fatalf("first reconciliation calls = put %v, delete %d", client.putCalls, client.deleteCalls)
			}

			if err := lambdaSetConcurrency(context.Background(), client, "function", &desired, false); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(client.putCalls, []int32{desired}) || client.deleteCalls != 0 {
				t.Fatalf("matching reconciliation calls = put %v, delete %d", client.putCalls, client.deleteCalls)
			}

			if err := lambdaSetConcurrency(context.Background(), client, "function", nil, false); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(client.putCalls, []int32{desired}) || client.deleteCalls != 1 {
				t.Fatalf("removal reconciliation calls = put %v, delete %d", client.putCalls, client.deleteCalls)
			}

			if err := lambdaSetConcurrency(context.Background(), client, "function", nil, false); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(client.putCalls, []int32{desired}) || client.deleteCalls != 1 {
				t.Fatalf("matching unreserved reconciliation calls = put %v, delete %d", client.putCalls, client.deleteCalls)
			}
		})
	}
}

func TestLambdaSetConcurrencyChangesReservedZeroWithoutConflatingOmission(t *testing.T) {
	zero := int32(0)
	four := int32(4)
	client := &fakeLambdaConcurrencyClient{current: &zero}

	if err := lambdaSetConcurrency(context.Background(), client, "function", &four, false); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(client.putCalls, []int32{4}) || client.deleteCalls != 0 {
		t.Fatalf("zero-to-positive calls = put %v, delete %d", client.putCalls, client.deleteCalls)
	}

	if err := lambdaSetConcurrency(context.Background(), client, "function", &zero, false); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(client.putCalls, []int32{4, 0}) || client.deleteCalls != 0 {
		t.Fatalf("positive-to-zero calls = put %v, delete %d", client.putCalls, client.deleteCalls)
	}
}

func TestLambdaSetConcurrencyPreviewOnlyToleratesMissingFunction(t *testing.T) {
	zero := int32(0)
	missing := &lambdatypes.ResourceNotFoundException{Message: aws.String("missing")}
	client := &fakeLambdaConcurrencyClient{getErr: missing}
	if err := lambdaSetConcurrency(context.Background(), client, "function", &zero, true); err != nil {
		t.Fatalf("preview missing function: %v", err)
	}
	if len(client.putCalls) != 0 || client.deleteCalls != 0 {
		t.Fatalf("preview mutated concurrency: put %v, delete %d", client.putCalls, client.deleteCalls)
	}

	client = &fakeLambdaConcurrencyClient{getErr: errors.New("access denied")}
	if err := lambdaSetConcurrency(context.Background(), client, "function", &zero, true); err == nil || !strings.Contains(err.Error(), "access denied") {
		t.Fatalf("preview non-not-found error = %v", err)
	}

	client = &fakeLambdaConcurrencyClient{getErr: missing}
	if err := lambdaSetConcurrency(context.Background(), client, "function", &zero, false); err == nil {
		t.Fatal("non-preview missing function unexpectedly succeeded")
	}
}

func TestLambdaSetConcurrencyRejectsInvalidDesiredStateAndNilSDKOutput(t *testing.T) {
	negative := int32(-1)
	client := &fakeLambdaConcurrencyClient{}
	if err := lambdaSetConcurrency(context.Background(), client, "function", &negative, false); err == nil {
		t.Fatal("negative concurrency unexpectedly succeeded")
	}
	if client.getCalls != 0 {
		t.Fatalf("negative concurrency made %d GetFunctionConcurrency calls", client.getCalls)
	}

	client = &fakeLambdaConcurrencyClient{nilGetOutput: true}
	if err := lambdaSetConcurrency(context.Background(), client, "function", nil, false); err == nil {
		t.Fatal("nil GetFunctionConcurrency output unexpectedly succeeded")
	}
}

func TestParseLambdaConcurrency(t *testing.T) {
	for _, test := range []struct {
		input string
		want  int32
	}{
		{input: "0", want: 0},
		{input: "5", want: 5},
		{input: "2147483647", want: 2147483647},
	} {
		t.Run(test.input, func(t *testing.T) {
			got, err := parseLambdaConcurrency(test.input)
			if err != nil {
				t.Fatal(err)
			}
			if got == nil || *got != test.want {
				t.Fatalf("parseLambdaConcurrency(%q) = %v, want %d", test.input, got, test.want)
			}
		})
	}

	for _, input := range []string{"", "invalid", "-1", "2147483648"} {
		t.Run("invalid "+input, func(t *testing.T) {
			if got, err := parseLambdaConcurrency(input); err == nil || got != nil {
				t.Fatalf("parseLambdaConcurrency(%q) = %v, %v; want error", input, got, err)
			}
		})
	}
}

func TestLambdaConcurrencyIntegration(t *testing.T) {
	if os.Getenv("LIBAWS_INTEGRATION") != "1" {
		t.Skip("set LIBAWS_INTEGRATION=1 to run AWS integration tests")
	}
	functionName := os.Getenv("LIBAWS_LAMBDA_CONCURRENCY_TEST_FUNCTION")
	if !strings.HasPrefix(functionName, "test-lambda-") || len(functionName) == len("test-lambda-") {
		t.Fatalf("LIBAWS_LAMBDA_CONCURRENCY_TEST_FUNCTION must name a unique test-lambda-* function, got %q", functionName)
	}
	expectedAccount := os.Getenv("LIBAWS_TEST_ACCOUNT")
	if expectedAccount == "" {
		t.Fatal("LIBAWS_TEST_ACCOUNT must identify the authorized scratch account")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	actualAccount, err := StsAccount(ctx)
	if err != nil {
		t.Fatalf("verify AWS account: %v", err)
	}
	if actualAccount != expectedAccount {
		t.Fatalf("refusing Lambda concurrency integration test in AWS account %q; expected scratch account %q", actualAccount, expectedAccount)
	}

	client := LambdaClient()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		if cleanupErr := LambdaSetConcurrency(cleanupCtx, functionName, nil, false); cleanupErr != nil {
			t.Errorf("restore unreserved concurrency for %q: %v", functionName, cleanupErr)
		}
	})

	assertCurrent := func(want *int32) {
		t.Helper()
		out, getErr := client.GetFunctionConcurrency(ctx, &lambda.GetFunctionConcurrencyInput{
			FunctionName: aws.String(functionName),
		})
		if getErr != nil {
			t.Fatalf("get function concurrency: %v", getErr)
		}
		if out == nil {
			t.Fatal("GetFunctionConcurrency returned nil output")
		}
		got := out.ReservedConcurrentExecutions
		if (got == nil) != (want == nil) || got != nil && *got != *want {
			t.Fatalf("reserved concurrency = %v, want %v", got, want)
		}
	}

	if err := LambdaSetConcurrency(ctx, functionName, nil, false); err != nil {
		t.Fatal(err)
	}
	assertCurrent(nil)
	if err := LambdaSetConcurrency(ctx, functionName, nil, false); err != nil {
		t.Fatalf("second unreserved reconciliation: %v", err)
	}

	zero := int32(0)
	if err := LambdaSetConcurrency(ctx, functionName, &zero, false); err != nil {
		t.Fatal(err)
	}
	assertCurrent(&zero)
	if err := LambdaSetConcurrency(ctx, functionName, &zero, false); err != nil {
		t.Fatalf("second disabled reconciliation: %v", err)
	}
	_, invokeErr := client.Invoke(ctx, &lambda.InvokeInput{
		FunctionName: aws.String(functionName),
		Payload:      []byte(`{"foo":"bar"}`),
	})
	var throttled *lambdatypes.TooManyRequestsException
	if !errors.As(invokeErr, &throttled) {
		t.Fatalf("invoke disabled function error = %v, want TooManyRequestsException", invokeErr)
	}
	if throttled.Reason != lambdatypes.ThrottleReasonReservedFunctionConcurrentInvocationLimitExceeded {
		t.Fatalf("invoke disabled function throttle reason = %q", throttled.Reason)
	}
	t.Logf("disabled invocation throttle reason: %s", throttled.Reason)

	triggerChannel := make(chan *InfraTrigger)
	close(triggerChannel)
	listed, err := InfraListLambda(ctx, triggerChannel, functionName)
	if err != nil {
		t.Fatalf("list Lambda infrastructure: %v", err)
	}
	listedFunction, ok := listed[functionName]
	if !ok {
		t.Fatalf("InfraListLambda omitted %q", functionName)
	}
	if !slices.Contains(listedFunction.Attr, "concurrency=0") {
		t.Fatalf("InfraListLambda attrs = %v, want concurrency=0", listedFunction.Attr)
	}

	positive := int32(1)
	if err := LambdaSetConcurrency(ctx, functionName, &positive, false); err != nil {
		t.Fatal(err)
	}
	assertCurrent(&positive)
	if err := LambdaSetConcurrency(ctx, functionName, &positive, false); err != nil {
		t.Fatalf("second positive reconciliation: %v", err)
	}

	if err := LambdaSetConcurrency(ctx, functionName, nil, false); err != nil {
		t.Fatal(err)
	}
	assertCurrent(nil)
	if err := LambdaSetConcurrency(ctx, functionName, nil, false); err != nil {
		t.Fatalf("second final unreserved reconciliation: %v", err)
	}
}

var _ lambdaConcurrencyClient = (*fakeLambdaConcurrencyClient)(nil)
