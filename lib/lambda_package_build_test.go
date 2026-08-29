package lib

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLambdaCreateZipGoBuildsCompleteEntrypointPackage(t *testing.T) {
	source := t.TempDir()
	for name, content := range map[string]string{
		"go.mod":    "module lambda-package-test\n\ngo 1.26\n",
		"main.go":   "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Print(message()) }\n",
		"helper.go": "package main\n\nfunc message() string { return \"complete-package\" }\n",
	} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	name := "libaws-test-" + filepath.Base(source)
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(LambdaZipFile(name))) })
	t.Setenv("LDFLAGS", " ")
	if err := lambdaCreateZipGo(&InfraLambda{
		Name:       name,
		Entrypoint: filepath.Join(source, "main.go"),
	}); err != nil {
		t.Fatalf("build Lambda package: %v", err)
	}
	output, err := exec.Command(filepath.Join(filepath.Dir(LambdaZipFile(name)), "bootstrap")).CombinedOutput()
	if err != nil {
		t.Fatalf("run built Lambda package: %v: %s", err, output)
	}
	if strings.TrimSpace(string(output)) != "complete-package" {
		t.Fatalf("built Lambda output = %q", output)
	}
}

func TestLambdaCreateZipGoRequiresDeclaredEntrypoint(t *testing.T) {
	source := t.TempDir()
	for name, content := range map[string]string{
		"go.mod":  "module lambda-entrypoint-test\n\ngo 1.26\n",
		"main.go": "package main\n\nfunc main() {}\n",
	} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	name := "libaws-test-" + filepath.Base(source)
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(LambdaZipFile(name))) })
	t.Setenv("LDFLAGS", " ")

	err := lambdaCreateZipGo(&InfraLambda{
		Name:       name,
		Entrypoint: filepath.Join(source, "missing.go"),
	})
	if err == nil || !strings.Contains(err.Error(), "entrypoint") {
		t.Fatalf("missing declared Lambda entrypoint error=%v", err)
	}
}
