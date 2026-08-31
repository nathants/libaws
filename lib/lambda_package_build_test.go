package lib

import (
	"archive/zip"
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

func TestLambdaPackageBuildRejectsSymlinkedRoot(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("LDFLAGS", " ")
	name := "libaws-test-symlink-root"
	packageRoot := filepath.Dir(filepath.Dir(LambdaZipFile(name)))
	if err := os.Symlink(t.TempDir(), packageRoot); err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	for fileName, content := range map[string]string{
		"go.mod":  "module lambda-package-root-test\n\ngo 1.26\n",
		"main.go": "package main\n\nfunc main() {}\n",
	} {
		if err := os.WriteFile(filepath.Join(source, fileName), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	err := lambdaCreateZipGo(&InfraLambda{Name: name, Entrypoint: filepath.Join(source, "main.go")})
	if err == nil || !strings.Contains(err.Error(), "package root") {
		t.Fatalf("symlinked Lambda package root error = %v", err)
	}
}

func TestNormalizeLambdaPackageUsesCanonicalCompression(t *testing.T) {
	writePackage := func(zipFile string, method uint16) {
		t.Helper()
		file, err := os.Create(zipFile)
		if err != nil {
			t.Fatal(err)
		}
		writer := zip.NewWriter(file)
		header := &zip.FileHeader{Name: "payload.txt", Method: method, Modified: time.Unix(946684800, 0)}
		header.SetMode(0o644)
		destination, err := writer.CreateHeader(header)
		if err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if _, err := destination.Write(bytes.Repeat([]byte("canonical Lambda package\n"), 4096)); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if err := normalizeLambdaPackage(zipFile); err != nil {
			t.Fatal(err)
		}
	}

	root := t.TempDir()
	stored := filepath.Join(root, "stored.zip")
	deflated := filepath.Join(root, "deflated.zip")
	writePackage(stored, zip.Store)
	writePackage(deflated, zip.Deflate)
	storedBytes, err := os.ReadFile(stored)
	if err != nil {
		t.Fatal(err)
	}
	deflatedBytes, err := os.ReadFile(deflated)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(storedBytes, deflatedBytes) {
		t.Fatal("identical Lambda contents retained source-dependent compression")
	}
	reader, err := zip.OpenReader(stored)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	if len(reader.File) != 1 || reader.File[0].Method != zip.Deflate {
		t.Fatalf("canonical Lambda compression method = %v", reader.File)
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

func TestLambdaPythonPackageIsByteReproducible(t *testing.T) {
	source := t.TempDir()
	dependency := filepath.Join(source, "dependency")
	if err := os.Mkdir(dependency, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"main.py": "def main(event, context):\n    return 'ok'\n",
		"dependency/setup.py": `from setuptools import setup
setup(name="lambda-reproducibility-dependency", version="0.0.1", py_modules=["dependency"])
`,
		"dependency/dependency.py": "VALUE = 'stable dependency'\n",
	} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	name := "libaws-test-python-" + sha256Hex([]byte(source))[:12]
	infraLambda := &InfraLambda{
		Name:       name,
		Entrypoint: filepath.Join(source, "main.py"),
		Require:    []string{dependency},
		dir:        source,
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(LambdaZipFile(name))) })
	build := func() []byte {
		t.Helper()
		if err := lambdaCreateZipPy(infraLambda); err != nil {
			t.Fatalf("build Python Lambda package: %v", err)
		}
		if err := LambdaIncludeInZip(infraLambda); err != nil {
			t.Fatalf("finalize Python Lambda package: %v", err)
		}
		data, err := LambdaZipBytes(infraLambda)
		if err != nil {
			t.Fatal(err)
		}
		reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range reader.File {
			if strings.Contains(entry.Name, "__pycache__/") || strings.HasSuffix(entry.Name, ".pyc") ||
				strings.HasSuffix(entry.Name, ".pyo") || strings.HasSuffix(entry.Name, ".virtualenv") {
				t.Fatalf("Python Lambda package contains build artifact: %s", entry.Name)
			}
		}
		return data
	}

	first := build()
	time.Sleep(2100 * time.Millisecond)
	second := build()
	if !bytes.Equal(first, second) {
		firstEntries, firstErr := zipSha256Hex(first)
		secondEntries, secondErr := zipSha256Hex(second)
		t.Fatalf(
			"identical Python Lambda inputs produced different package bytes: first=%v (%v), second=%v (%v)",
			firstEntries, firstErr, secondEntries, secondErr,
		)
	}
}
