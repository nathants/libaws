package lib

import (
	"os"
	"path/filepath"
	"testing"
)

func quickLambdaForTest(t *testing.T, runtime string) *InfraLambda {
	t.Helper()
	tempDir := t.TempDir()
	name := "libaws-test-" + filepath.Base(filepath.Dir(tempDir)) + "-" + filepath.Base(tempDir)
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(LambdaZipFile(name))) })
	return &InfraLambda{Name: name, runtime: runtime}
}

func TestLambdaPrepareQuickPackageUpdatesGoPackage(t *testing.T) {
	infraLambda := quickLambdaForTest(t, lambdaRuntimeGo)
	updateCalls := 0
	err := lambdaPrepareQuickPackage(
		infraLambda,
		func(*InfraLambda) error {
			updateCalls++
			return nil
		},
		func(*InfraLambda) error {
			t.Fatal("quick Go update rebuilt the package")
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if updateCalls != 1 {
		t.Fatalf("quick Go package updates=%d, want 1", updateCalls)
	}
}

func TestLambdaPrepareQuickPackageBuildsMissingPythonPackage(t *testing.T) {
	infraLambda := quickLambdaForTest(t, lambdaRuntimePython)
	createCalls := 0
	err := lambdaPrepareQuickPackage(
		infraLambda,
		func(*InfraLambda) error {
			t.Fatal("quick Python update tried to patch a missing package")
			return nil
		},
		func(*InfraLambda) error {
			createCalls++
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if createCalls != 1 {
		t.Fatalf("quick Python package builds=%d, want 1", createCalls)
	}
}
