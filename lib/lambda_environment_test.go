package lib

import (
	"strings"
	"testing"
)

func TestLambdaEnvironmentVariablesEnforcesAWSSizeLimit(t *testing.T) {
	atLimit, err := lambdaEnvironmentVariables([]string{"AA=" + strings.Repeat("x", lambdaEnvironmentMaxBytes-2)})
	if err != nil {
		t.Fatalf("environment at AWS limit was rejected: %v", err)
	}
	if len(atLimit) != 1 {
		t.Fatalf("environment entries=%d, want 1", len(atLimit))
	}

	_, err = lambdaEnvironmentVariables([]string{"AA=" + strings.Repeat("x", lambdaEnvironmentMaxBytes-1)})
	if err == nil || !strings.Contains(err.Error(), "exceeds AWS Lambda") {
		t.Fatalf("oversized Lambda environment error=%v", err)
	}
}

func TestLambdaEnvironmentVariablesRejectsDuplicateNames(t *testing.T) {
	_, err := lambdaEnvironmentVariables([]string{"AA=first", "AA=second"})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate Lambda environment error=%v", err)
	}
}

func TestLambdaEnvironmentVariablesRejectsInvalidNames(t *testing.T) {
	for _, name := range []string{"", "A", "_A", "A-B"} {
		_, err := lambdaEnvironmentVariables([]string{name + "=value"})
		if err == nil || !strings.Contains(err.Error(), "names must match") {
			t.Fatalf("invalid Lambda environment name %q error=%v", name, err)
		}
	}
}
