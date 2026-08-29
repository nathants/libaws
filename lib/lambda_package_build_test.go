package lib

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestLambdaGoPackageIsByteReproducible(t *testing.T) {
	root := t.TempDir()
	writeSource := func(source string) {
		t.Helper()
		if err := os.Mkdir(source, 0o700); err != nil {
			t.Fatal(err)
		}
		for name, content := range map[string]string{
			"go.mod":    "module lambda-reproducibility-test\n\ngo 1.26\n",
			"main.go":   "package main\n\nfunc main() {}\n",
			"asset.txt": "identical included asset",
		} {
			if err := os.WriteFile(filepath.Join(source, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	gitSource := filepath.Join(root, "git-source")
	plainSource := filepath.Join(root, "plain-source")
	writeSource(gitSource)
	writeSource(plainSource)
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "."},
		{"-c", "user.name=libaws test", "-c", "user.email=libaws@example.invalid", "commit", "-qm", "initial"},
	} {
		command := exec.Command("git", args...)
		command.Dir = gitSource
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("initialize test Git source: %v: %s", err, output)
		}
	}
	t.Setenv("LDFLAGS", " ")

	build := func(source, name string, modified time.Time) []byte {
		t.Helper()
		asset := filepath.Join(source, "asset.txt")
		if err := os.Chtimes(asset, modified, modified); err != nil {
			t.Fatal(err)
		}
		infraLambda := &InfraLambda{
			Name:       name,
			Entrypoint: filepath.Join(source, "main.go"),
			Include:    []string{asset},
			dir:        source,
		}
		t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(LambdaZipFile(name))) })
		if err := lambdaCreateZipGo(infraLambda); err != nil {
			t.Fatalf("build Lambda package %s: %v", name, err)
		}
		if err := LambdaIncludeInZip(infraLambda); err != nil {
			t.Fatalf("finalize Lambda package %s: %v", name, err)
		}
		data, err := LambdaZipBytes(infraLambda)
		if err != nil {
			t.Fatalf("read Lambda package %s: %v", name, err)
		}
		return data
	}

	name := "libaws-test-" + filepath.Base(root)
	first := build(gitSource, name+"-git", time.Unix(946684800, 0))
	second := build(plainSource, name+"-plain", time.Unix(1735689600, 0))
	if !bytes.Equal(first, second) {
		firstEntries, firstErr := zipSha256Hex(first)
		secondEntries, secondErr := zipSha256Hex(second)
		t.Fatalf(
			"identical Lambda inputs produced different package bytes: first=%v (%v), second=%v (%v)",
			firstEntries, firstErr, secondEntries, secondErr,
		)
	}
}
