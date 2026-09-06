package lib

import (
	"archive/zip"
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

func writeQuickLambdaPackageForTest(t *testing.T, infraLambda *InfraLambda) {
	t.Helper()
	zipFile := LambdaZipFile(infraLambda.Name)
	if err := os.MkdirAll(filepath.Dir(zipFile), 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(zipFile)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	if err := writer.Close(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLambdaPrepareQuickPackageUpdatesGoPackage(t *testing.T) {
	infraLambda := quickLambdaForTest(t, lambdaRuntimeGo)
	updateCalls := 0
	err := lambdaPrepareQuickPackage(
		infraLambda,
		func(infraLambda *InfraLambda) error {
			updateCalls++
			writeQuickLambdaPackageForTest(t, infraLambda)
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
		func(infraLambda *InfraLambda) error {
			createCalls++
			writeQuickLambdaPackageForTest(t, infraLambda)
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

func TestLambdaPrepareQuickContainerNeedsNoZip(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	infraLambda := &InfraLambda{Name: "container-fixture", runtime: lambdaRuntimeContainer}
	calls := 0
	packageFn := func(*InfraLambda) error { calls++; return nil }
	if err := lambdaPrepareQuickPackage(infraLambda, packageFn, packageFn); err != nil {
		t.Fatalf("container update tried to package a ZIP: %v", err)
	}
	if calls != 0 || Exists(lambdaPackageRoot()) {
		t.Fatalf("container created local package state: calls=%d", calls)
	}
}
