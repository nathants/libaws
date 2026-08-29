package lib

import (
	"strings"
	"testing"
)

func lambdaValidationInput(entrypoint string) map[string]any {
	return map[string]any{
		"function": map[string]any{
			infraKeyLambdaEntrypoint: entrypoint,
		},
	}
}

func TestInfraParseValidateLambdaRequiresDigestQualifiedContainerImage(t *testing.T) {
	for _, entrypoint := range []string{
		"012345678901.dkr.ecr.us-east-1.amazonaws.com/function:latest",
		"012345678901.dkr.ecr.us-east-1.amazonaws.com/function@sha256:abc",
	} {
		if err := infraParseValidateLambda(lambdaValidationInput(entrypoint)); err == nil {
			t.Fatalf("mutable or malformed Lambda container image was accepted: %s", entrypoint)
		}
	}
}

func TestInfraParseValidateLambdaAcceptsDigestQualifiedContainerImage(t *testing.T) {
	entrypoint := "012345678901.dkr.ecr.us-east-1.amazonaws.com/function@sha256:" + strings.Repeat("a", 64)
	if err := infraParseValidateLambda(lambdaValidationInput(entrypoint)); err != nil {
		t.Fatalf("digest-qualified Lambda container image was rejected: %v", err)
	}
}
