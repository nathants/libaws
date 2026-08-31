package lib

import (
	"context"
	"path/filepath"
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

func TestInfraParseValidateLambdaRejectsInvalidFunctionName(t *testing.T) {
	input := map[string]any{
		"../../outside": map[string]any{
			infraKeyLambdaEntrypoint: "main.go",
		},
	}
	if err := infraParseValidateLambda(input); err == nil || !strings.Contains(err.Error(), "lambda name") {
		t.Fatalf("invalid Lambda name error = %v", err)
	}
}

func TestInfraEnsureLambdaRejectsInvalidFunctionNameBeforeProviderCalls(t *testing.T) {
	err := InfraEnsureLambda(context.Background(), &InfraSet{
		Lambda: map[string]*InfraLambda{
			"../../outside": {Entrypoint: "main.go"},
		},
	}, "", true, false)
	if err == nil || !strings.Contains(err.Error(), "lambda name") {
		t.Fatalf("invalid Lambda name ensure error = %v", err)
	}
}

func TestLambdaZipFileStaysInsideDedicatedTemporaryRoot(t *testing.T) {
	root := lambdaPackageRoot()
	zipFile := filepath.Clean(LambdaZipFile("../../outside"))
	relative, err := filepath.Rel(root, zipFile)
	if err != nil {
		t.Fatal(err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatalf("Lambda package path %q escaped %q", zipFile, root)
	}
}
