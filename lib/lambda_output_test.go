package lib

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

func TestLambdaFormatVariablesHashesValuesByDefault(t *testing.T) {
	variables := map[string]string{
		"Z_SECRET": "top-secret-z",
		"A_SECRET": "top-secret-a",
	}

	lines := LambdaFormatVariables(variables, false)
	if len(lines) != 2 {
		t.Fatalf("expected two variables, got %d", len(lines))
	}
	if !strings.HasPrefix(lines[0], "A_SECRET=sha256:") || !strings.HasPrefix(lines[1], "Z_SECRET=sha256:") {
		t.Fatalf("variables are not deterministically sorted and hashed: %#v", lines)
	}
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "top-secret") {
		t.Fatalf("default output leaked a variable value: %q", joined)
	}
}

func TestLambdaFormatVariablesShowsValuesOnlyWhenRequested(t *testing.T) {
	lines := LambdaFormatVariables(map[string]string{"SECRET": "requested-value"}, true)
	if len(lines) != 1 || lines[0] != "SECRET=requested-value" {
		t.Fatalf("explicit value output mismatch: %#v", lines)
	}
}

func TestLambdaSanitizeDescriptionProtectsEnvironmentAndCodeURL(t *testing.T) {
	out := &lambda.GetFunctionOutput{
		Code: &lambdatypes.FunctionCodeLocation{
			Location: aws.String("https://signed-download.example/secret-query"),
		},
		Configuration: &lambdatypes.FunctionConfiguration{
			Environment: &lambdatypes.EnvironmentResponse{
				Variables: map[string]string{"SECRET": "environment-secret"},
			},
		},
	}
	configuration := &lambda.GetFunctionConfigurationOutput{
		Environment: &lambdatypes.EnvironmentResponse{
			Variables: map[string]string{"SECRET": "configuration-secret"},
		},
	}

	LambdaSanitizeDescription(out, configuration, false)
	if got := aws.ToString(out.Code.Location); !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("code URL was not hashed: %q", got)
	}
	if got := out.Configuration.Environment.Variables["SECRET"]; !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("GetFunction environment was not hashed: %q", got)
	}
	if got := configuration.Environment.Variables["SECRET"]; !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("configuration environment was not hashed: %q", got)
	}
}

func TestLambdaSanitizeDescriptionExplicitValuesStillProtectsCodeURL(t *testing.T) {
	out := &lambda.GetFunctionOutput{
		Code: &lambdatypes.FunctionCodeLocation{
			Location: aws.String("https://signed-download.example/secret-query"),
		},
		Configuration: &lambdatypes.FunctionConfiguration{
			Environment: &lambdatypes.EnvironmentResponse{
				Variables: map[string]string{"SECRET": "environment-secret"},
			},
		},
	}
	configuration := &lambda.GetFunctionConfigurationOutput{
		Environment: &lambdatypes.EnvironmentResponse{
			Variables: map[string]string{"SECRET": "configuration-secret"},
		},
	}

	LambdaSanitizeDescription(out, configuration, true)
	if got := aws.ToString(out.Code.Location); !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("explicit environment output exposed code URL: %q", got)
	}
	if got := out.Configuration.Environment.Variables["SECRET"]; got != "environment-secret" {
		t.Fatalf("explicit GetFunction environment changed: %q", got)
	}
	if got := configuration.Environment.Variables["SECRET"]; got != "configuration-secret" {
		t.Fatalf("explicit configuration environment changed: %q", got)
	}
}
