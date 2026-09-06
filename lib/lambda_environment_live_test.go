package lib

import (
	"context"
	"errors"
	"maps"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/aws/smithy-go"
)

// examples/misc/basic owns this fixture and also exercises CLI create/preview/quick paths.
func TestLambdaEnvironmentIntegration(t *testing.T) {
	name := os.Getenv("LIBAWS_LAMBDA_ENVIRONMENT_TEST_FUNCTION")
	if name == "" {
		t.Skip("run through examples/misc/basic to provide a unique Lambda fixture")
	}
	requireLiveAWSAccount(t)
	if !strings.HasPrefix(name, "test-lambda-") || name == "test-lambda-" {
		t.Fatal("environment integration requires a unique test-lambda-* fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	client := LambdaClient()
	current, err := client.GetFunctionConfiguration(ctx, &lambda.GetFunctionConfigurationInput{FunctionName: aws.String(name)})
	if err != nil {
		t.Fatal(err)
	}
	tags, err := client.ListTags(ctx, &lambda.ListTagsInput{Resource: current.FunctionArn})
	if err != nil {
		t.Fatal(err)
	}
	if tags.Tags[infraSetTagName] != "test-infraset-"+strings.TrimPrefix(name, "test-lambda-") {
		t.Fatal("Lambda is not owned by the basic example's unique infrastructure set")
	}
	if current.Environment == nil || current.Environment.Error != nil || aws.ToInt32(current.Timeout) != 60 || aws.ToInt32(current.MemorySize) != 128 {
		t.Fatal("unexpected basic example configuration")
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		if err := lambdaEnsureFunctionConfigurationWithClient(
			cleanupCtx, client, &InfraLambda{Name: name}, current.Environment.Variables,
			60, 128, false, false, time.Minute,
		); err != nil {
			t.Errorf("restore example environment: %v", err)
		}
	})
	assertEnvironment := func(t *testing.T, want map[string]string) {
		t.Helper()
		out, err := client.GetFunctionConfiguration(ctx, &lambda.GetFunctionConfigurationInput{FunctionName: aws.String(name)})
		if err != nil {
			t.Fatal(err)
		}
		if out.Environment == nil || out.Environment.Error != nil || !maps.Equal(out.Environment.Variables, want) {
			t.Fatal("AWS environment differs from the accepted declaration")
		}
	}
	for _, test := range []struct {
		name, value   string
		emptyVariable bool
	}{
		{"ASCII", strings.Repeat("x", 4087), false},
		{"HTML", strings.Repeat("<>&", 1362) + "x", false},
		{"UTF8", strings.Repeat("é", 2043) + "x", false},
		{"line separator", "\u2028" + strings.Repeat("x", 4084), false},
		{"paragraph separator", "\u2029" + strings.Repeat("x", 4084), false},
		{"JSON escapes", "\"\\\n\b\f" + strings.Repeat("x", 4077), false},
		{"long control escape", "\x01" + strings.Repeat("x", 4081), false},
		{"multiple variables", strings.Repeat("x", 4079), true},
	} {
		t.Run("quota/"+test.name, func(t *testing.T) {
			entries := []string{"AA=" + test.value}
			if test.emptyVariable {
				entries = append(entries, "BB=")
			}
			variables, err := lambdaEnvironmentVariables(entries)
			if err != nil {
				t.Fatalf("local rejection at AWS quota: %v", err)
			}
			if err := lambdaEnsureFunctionConfigurationWithClient(ctx, client, &InfraLambda{Name: name}, variables, 60, 128, false, false, time.Minute); err != nil {
				t.Fatalf("AWS rejection at environment quota: %v", err)
			}
			entries[0] += "x"
			if _, err := lambdaEnvironmentVariables(entries); err == nil || !strings.Contains(err.Error(), "size 4097 bytes") {
				t.Fatalf("local rejection above AWS quota: %v", err)
			}
			oversized := maps.Clone(variables)
			oversized["AA"] += "x"
			_, err = client.UpdateFunctionConfiguration(ctx, &lambda.UpdateFunctionConfigurationInput{
				FunctionName: aws.String(name), Environment: &lambdatypes.Environment{Variables: oversized},
				Timeout: aws.Int32(60), MemorySize: aws.Int32(128),
			})
			var invalid *lambdatypes.InvalidParameterValueException
			if !errors.As(err, &invalid) || !strings.Contains(aws.ToString(invalid.Message), "4KB") {
				t.Fatalf("AWS rejection above environment quota: %v", err)
			}
			assertEnvironment(t, variables)
		})
	}
	for _, test := range []struct{ name, character string }{
		{"backspace", "\b"}, {"form feed", "\f"}, {"line separator", "\u2028"}, {"paragraph separator", "\u2029"},
	} {
		t.Run("request/"+test.name, func(t *testing.T) {
			// The real SDK boundary unit test checks this full request's 5120 bytes.
			variables, err := lambdaEnvironmentVariables([]string{"AA=" + strings.Repeat(test.character, 800) + strings.Repeat("x", 251)})
			if err != nil {
				t.Fatal(err)
			}
			if err := lambdaEnsureFunctionConfigurationWithClient(ctx, client, &InfraLambda{Name: name}, variables, 60, 128, false, false, time.Minute); err != nil {
				t.Fatalf("AWS rejection at request limit: %v", err)
			}
			oversized := maps.Clone(variables)
			oversized["AA"] += "x"
			err = lambdaEnsureFunctionConfigurationWithClient(ctx, client, &InfraLambda{Name: name}, oversized, 60, 128, false, false, time.Minute)
			if err == nil || !strings.Contains(err.Error(), "request size 5121 bytes") {
				t.Fatalf("local rejection above request limit: %v", err)
			}
			_, err = client.UpdateFunctionConfiguration(ctx, &lambda.UpdateFunctionConfigurationInput{
				FunctionName: aws.String(name), Environment: &lambdatypes.Environment{Variables: oversized},
				Timeout: aws.Int32(60), MemorySize: aws.Int32(128),
			})
			var apiErr smithy.APIError
			if !errors.As(err, &apiErr) || apiErr.ErrorCode() != "RequestEntityTooLargeException" {
				t.Fatalf("AWS rejection above request limit: %v", err)
			}
			assertEnvironment(t, variables)
		})
	}
}
