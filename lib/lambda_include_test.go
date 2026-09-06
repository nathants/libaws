package lib

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLambdaIncludesResolveBesideDeclaration(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	for _, kind := range []string{"file", "glob", "symlink", "absolute", "missing"} {
		t.Run(kind, func(t *testing.T) {
			source := t.TempDir()
			if err := os.WriteFile(filepath.Join(source, "asset.txt"), []byte("declaration asset"), 0o600); err != nil {
				t.Fatal(err)
			}
			include, archiveName, want := "asset.txt", "asset.txt", "declaration asset"
			switch kind {
			case "glob":
				include = "*.txt"
			case "symlink":
				include, archiveName, want = "link", "link", "asset.txt"
				if err := os.Symlink("asset.txt", filepath.Join(source, "link")); err != nil {
					t.Fatal(err)
				}
			case "absolute":
				include = filepath.Join(source, "asset.txt")
			case "missing":
				include = "missing.txt"
			default:
			}
			data := "name: test-includes\nlambda:\n  include-fixture:\n    entrypoint: main.go\n    include:\n      - " + Json(include) + "\n"
			if err := os.WriteFile(filepath.Join(source, "infra.yaml"), []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			caller := t.TempDir()
			// A same-named caller file must not substitute for a missing declaration asset.
			if err := os.WriteFile(filepath.Join(caller, "missing.txt"), []byte("wrong caller asset"), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Chdir(caller)
			infra, err := InfraParse(filepath.Join(source, "infra.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			function := infra.Lambda["include-fixture"]
			function.Name = "include-fixture"
			if _, err := resetLambdaPackageDir(function.Name); err != nil {
				t.Fatal(err)
			}
			file, err := os.Create(LambdaZipFile(function.Name))
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
			err = LambdaIncludeInZip(function)
			if kind == "missing" {
				if err == nil || !strings.Contains(err.Error(), "no such path for include: missing.txt") {
					t.Fatalf("expected local missing declaration asset error: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("include beside infra.yaml depends on caller cwd: %v", err)
			}
			reader, err := zip.OpenReader(LambdaZipFile(function.Name))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = reader.Close() }()
			if len(reader.File) != 1 || reader.File[0].Name != archiveName {
				t.Fatalf("include archive path changed: %+v", reader.File)
			}
			entry, err := reader.File[0].Open()
			if err != nil {
				t.Fatal(err)
			}
			content, err := io.ReadAll(entry)
			_ = entry.Close()
			if err != nil || string(content) != want {
				t.Fatalf("wrong include content: %q err=%v", content, err)
			}
			if kind == "symlink" && reader.File[0].Mode()&os.ModeSymlink == 0 {
				t.Fatal("include symlink was dereferenced")
			}
		})
	}
}
