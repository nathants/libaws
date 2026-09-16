package lib

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestGoLambda(t *testing.T, message string) (source, entrypoint string) {
	t.Helper()
	source = t.TempDir()
	for name, content := range map[string]string{
		"go.mod":  "module libaws-build-only-test\n\ngo 1.27\n",
		"main.go": "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Print(\"" + message + "\") }\n",
	} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return source, filepath.Join(source, "main.go")
}

func commitTestRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "."},
		{"-c", "user.name=libaws test", "-c", "user.email=libaws@example.invalid", "commit", "-qm", "initial"},
	} {
		command := exec.Command("git", args...)
		command.Dir = dir
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("initialize test Git source: %v: %s", err, output)
		}
	}
}

func gitHead(t *testing.T, dir string) string {
	t.Helper()
	command := exec.Command("git", "rev-parse", "HEAD")
	command.Dir = dir
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(output))
}

func readBuildManifest(t *testing.T, dir string) InfraBuildManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, infraBuildManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	manifest := InfraBuildManifest{}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func runBinary(t *testing.T, path string) string {
	t.Helper()
	output, err := exec.Command(path).CombinedOutput()
	if err != nil {
		t.Fatalf("run %s: %v: %s", path, err, output)
	}
	return string(output)
}

func TestInfraBuildOnlyBuildsArtifactAndManifest(t *testing.T) {
	source, entrypoint := writeTestGoLambda(t, "artifact-fixture")
	commitTestRepo(t, source)
	name := "libaws-test-build-only"
	buildDir := filepath.Join(t.TempDir(), "artifacts")
	t.Setenv("LDFLAGS", " ")
	infraSet := &InfraSet{Lambda: map[string]*InfraLambda{
		name:             {Entrypoint: entrypoint},
		name + "-python": {Entrypoint: filepath.Join(source, "main.py")},
	}}
	if err := InfraBuildOnly(infraSet, "", buildDir); err != nil {
		t.Fatalf("build artifacts: %v", err)
	}
	manifest := readBuildManifest(t, buildDir)
	if manifest.Version != infraBuildManifestVersion {
		t.Fatalf("manifest version = %d", manifest.Version)
	}
	if len(manifest.Artifacts) != 1 {
		t.Fatalf("manifest artifacts = %v, want only the Go Lambda", manifest.Artifacts)
	}
	artifact := manifest.Artifacts[0]
	if artifact.Name != name || artifact.File != name+"/"+infraBuildBootstrapName {
		t.Fatalf("manifest artifact identity = %+v", artifact)
	}
	if artifact.GitCommit != gitHead(t, source) {
		t.Fatalf("manifest artifact commit = %q", artifact.GitCommit)
	}
	if artifact.GitDirty {
		t.Fatal("clean test source was recorded as dirty")
	}
	bootstrap := filepath.Join(buildDir, name, infraBuildBootstrapName)
	hash, err := sha256File(bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	if hash != artifact.Sha256 {
		t.Fatalf("manifest sha256 = %s, file sha256 = %s", artifact.Sha256, hash)
	}
	if output := runBinary(t, bootstrap); output != "artifact-fixture" {
		t.Fatalf("built artifact output = %q", output)
	}
}

func TestInfraBuildOnlyRecordsDirtySource(t *testing.T) {
	source, entrypoint := writeTestGoLambda(t, "artifact-fixture")
	commitTestRepo(t, source)
	if err := os.WriteFile(entrypoint, []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	name := "libaws-test-build-only-dirty"
	buildDir := filepath.Join(t.TempDir(), "artifacts")
	t.Setenv("LDFLAGS", " ")
	infraSet := &InfraSet{Lambda: map[string]*InfraLambda{name: {Entrypoint: entrypoint}}}
	if err := InfraBuildOnly(infraSet, "", buildDir); err != nil {
		t.Fatalf("build artifacts: %v", err)
	}
	manifest := readBuildManifest(t, buildDir)
	if len(manifest.Artifacts) != 1 || !manifest.Artifacts[0].GitDirty {
		t.Fatalf("dirty source manifest artifacts = %+v", manifest.Artifacts)
	}
}

func TestInfraBuildOnlyRequiresAnArtifactToBuild(t *testing.T) {
	source, _ := writeTestGoLambda(t, "artifact-fixture")
	name := "libaws-test-build-only-python"
	infraSet := &InfraSet{Lambda: map[string]*InfraLambda{
		name: {Entrypoint: filepath.Join(source, "main.py")},
	}}
	err := InfraBuildOnly(infraSet, "", filepath.Join(t.TempDir(), "artifacts"))
	if err == nil || !strings.Contains(err.Error(), "no Lambda artifacts") {
		t.Fatalf("artifact-free build error = %v", err)
	}
}

func TestInfraBuildOnlyRequiresCommittedSourceIdentity(t *testing.T) {
	_, entrypoint := writeTestGoLambda(t, "artifact-fixture")
	name := "libaws-test-build-only-no-git"
	buildDir := filepath.Join(t.TempDir(), "artifacts")
	t.Setenv("LDFLAGS", " ")
	infraSet := &InfraSet{Lambda: map[string]*InfraLambda{name: {Entrypoint: entrypoint}}}
	err := InfraBuildOnly(infraSet, "", buildDir)
	if err == nil || !strings.Contains(err.Error(), "resolve source commit") {
		t.Fatalf("source identity error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(buildDir, infraBuildManifestFile)); !os.IsNotExist(statErr) {
		t.Fatalf("failed build left a manifest: %v", statErr)
	}
}

func TestInfraBuildOnlyRemovesStaleManifestOnFailure(t *testing.T) {
	source, entrypoint := writeTestGoLambda(t, "artifact-fixture")
	commitTestRepo(t, source)
	name := "libaws-test-build-only-stale"
	buildDir := filepath.Join(t.TempDir(), "artifacts")
	t.Setenv("LDFLAGS", " ")
	infraSet := &InfraSet{Lambda: map[string]*InfraLambda{name: {Entrypoint: entrypoint}}}
	if err := InfraBuildOnly(infraSet, "", buildDir); err != nil {
		t.Fatalf("build artifacts: %v", err)
	}
	if err := os.Remove(entrypoint); err != nil {
		t.Fatal(err)
	}
	if err := InfraBuildOnly(infraSet, "", buildDir); err == nil {
		t.Fatal("build without a declared entrypoint succeeded")
	}
	if _, err := os.Stat(filepath.Join(buildDir, infraBuildManifestFile)); !os.IsNotExist(err) {
		t.Fatalf("failed build left a stale manifest: %v", err)
	}
}

func TestLambdaPackageFromArtifactMatchesDirectBuild(t *testing.T) {
	source, entrypoint := writeTestGoLambda(t, "artifact-fixture")
	commitTestRepo(t, source)
	directName := "libaws-test-direct-build"
	artifactName := "libaws-test-artifact-build"
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Dir(LambdaZipFile(directName)))
		_ = os.RemoveAll(filepath.Dir(LambdaZipFile(artifactName)))
	})
	t.Setenv("LDFLAGS", " ")

	direct := &InfraLambda{Name: directName, Entrypoint: entrypoint}
	if err := lambdaCreateZipGo(direct); err != nil {
		t.Fatalf("build direct package: %v", err)
	}
	if err := LambdaIncludeInZip(direct); err != nil {
		t.Fatalf("finalize direct package: %v", err)
	}
	directBytes, err := LambdaZipBytes(direct)
	if err != nil {
		t.Fatal(err)
	}

	buildDir := filepath.Join(t.TempDir(), "artifacts")
	infraSet := &InfraSet{Lambda: map[string]*InfraLambda{artifactName: {Entrypoint: entrypoint}}}
	if err := InfraBuildOnly(infraSet, "", buildDir); err != nil {
		t.Fatalf("build artifacts: %v", err)
	}
	artifacts, err := loadInfraBuildArtifacts(buildDir)
	if err != nil {
		t.Fatal(err)
	}
	artifact := &InfraLambda{Name: artifactName, Entrypoint: entrypoint}
	if err := artifacts.packageGoLambda(artifact); err != nil {
		t.Fatalf("package artifact: %v", err)
	}
	if err := LambdaIncludeInZip(artifact); err != nil {
		t.Fatalf("finalize artifact package: %v", err)
	}
	artifactBytes, err := LambdaZipBytes(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(directBytes, artifactBytes) {
		directEntries, directErr := zipSha256Hex(directBytes)
		artifactEntries, artifactErr := zipSha256Hex(artifactBytes)
		t.Fatalf("artifact package differs from direct build: direct=%v (%v), artifact=%v (%v)",
			directEntries, directErr, artifactEntries, artifactErr)
	}
}

func TestLambdaPackageFromArtifactIgnoresEntrypointSource(t *testing.T) {
	source, entrypoint := writeTestGoLambda(t, "artifact-fixture")
	commitTestRepo(t, source)
	name := "libaws-test-artifact-source-free"
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(LambdaZipFile(name))) })
	t.Setenv("LDFLAGS", " ")
	buildDir := filepath.Join(t.TempDir(), "artifacts")
	infraSet := &InfraSet{Lambda: map[string]*InfraLambda{name: {Entrypoint: entrypoint}}}
	if err := InfraBuildOnly(infraSet, "", buildDir); err != nil {
		t.Fatalf("build artifacts: %v", err)
	}
	if err := os.RemoveAll(source); err != nil {
		t.Fatal(err)
	}
	artifacts, err := loadInfraBuildArtifacts(buildDir)
	if err != nil {
		t.Fatal(err)
	}
	// The entrypoint no longer exists; packaging must use only the artifact.
	infraLambda := &InfraLambda{Name: name, Entrypoint: entrypoint}
	if err := artifacts.packageGoLambda(infraLambda); err != nil {
		t.Fatalf("package artifact without source: %v", err)
	}
	if !artifacts.consumed[name] {
		t.Fatal("artifact verification was not recorded")
	}
	bootstrapBytes, err := os.ReadFile(filepath.Join(buildDir, name, infraBuildBootstrapName))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.OpenReader(LambdaZipFile(name))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	var packaged []byte
	for _, entry := range reader.File {
		if filepath.Base(entry.Name) != infraBuildBootstrapName {
			t.Fatalf("unexpected package entry: %s", entry.Name)
		}
		content, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		packaged, err = io.ReadAll(content)
		closeErr := content.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("read package entry: %v %v", err, closeErr)
		}
	}
	if !bytes.Equal(packaged, bootstrapBytes) {
		t.Fatal("packaged bootstrap differs from the verified artifact")
	}
	executable := filepath.Join(t.TempDir(), infraBuildBootstrapName)
	if err := os.WriteFile(executable, packaged, 0o755); err != nil {
		t.Fatal(err)
	}
	if output := runBinary(t, executable); output != "artifact-fixture" {
		t.Fatalf("packaged artifact output = %q", output)
	}
}

func TestLambdaPackageFromArtifactRejectsTamperedBytes(t *testing.T) {
	source, entrypoint := writeTestGoLambda(t, "artifact-fixture")
	commitTestRepo(t, source)
	name := "libaws-test-artifact-tampered"
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(LambdaZipFile(name))) })
	t.Setenv("LDFLAGS", " ")
	buildDir := filepath.Join(t.TempDir(), "artifacts")
	infraSet := &InfraSet{Lambda: map[string]*InfraLambda{name: {Entrypoint: entrypoint}}}
	if err := InfraBuildOnly(infraSet, "", buildDir); err != nil {
		t.Fatalf("build artifacts: %v", err)
	}
	bootstrap := filepath.Join(buildDir, name, infraBuildBootstrapName)
	file, err := os.OpenFile(bootstrap, os.O_WRONLY|os.O_APPEND, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("tampered")); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	artifacts, err := loadInfraBuildArtifacts(buildDir)
	if err != nil {
		t.Fatal(err)
	}
	err = artifacts.packageGoLambda(&InfraLambda{Name: name})
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("tampered artifact error = %v", err)
	}
	if err := os.Remove(bootstrap); err != nil {
		t.Fatal(err)
	}
	artifacts, err = loadInfraBuildArtifacts(buildDir)
	if err != nil {
		t.Fatal(err)
	}
	err = artifacts.packageGoLambda(&InfraLambda{Name: name})
	if err == nil || !strings.Contains(err.Error(), "inspect Lambda artifact") {
		t.Fatalf("missing artifact error = %v", err)
	}
	err = artifacts.packageGoLambda(&InfraLambda{Name: "libaws-test-artifact-unknown"})
	if err == nil || !strings.Contains(err.Error(), "no artifact for Lambda") {
		t.Fatalf("undeclared artifact error = %v", err)
	}
}

func TestLoadInfraBuildArtifactsRejectsInvalidManifests(t *testing.T) {
	valid := func() InfraBuildManifest {
		return InfraBuildManifest{Version: infraBuildManifestVersion, Artifacts: []InfraBuildArtifact{
			{Name: "libaws-test-manifest", File: "libaws-test-manifest/bootstrap", Sha256: strings.Repeat("a", 64)},
		}}
	}
	cases := []struct {
		name     string
		manifest any
		want     string
	}{
		{"not-json", "not json", "parse build manifest"},
		{"version", func() InfraBuildManifest { m := valid(); m.Version = 2; return m }(), "version"},
		{"empty", InfraBuildManifest{Version: infraBuildManifestVersion}, "lists no artifacts"},
		{"duplicate", func() InfraBuildManifest { m := valid(); m.Artifacts = append(m.Artifacts, m.Artifacts[0]); return m }(), "duplicate"},
		{"escaping-file", func() InfraBuildManifest { m := valid(); m.Artifacts[0].File = "../escape"; return m }(), "clean relative path"},
		{"absolute-file", func() InfraBuildManifest { m := valid(); m.Artifacts[0].File = "/absolute"; return m }(), "clean relative path"},
		{"invalid-sha", func() InfraBuildManifest { m := valid(); m.Artifacts[0].Sha256 = strings.Repeat("A", 64); return m }(), "sha256"},
		{"invalid-name", func() InfraBuildManifest { m := valid(); m.Artifacts[0].Name = "../../outside"; return m }(), "lambda name"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := t.TempDir()
			var data []byte
			var err error
			switch value := testCase.manifest.(type) {
			case string:
				data = []byte(value)
			default:
				data, err = json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, infraBuildManifestFile), data, 0o600); err != nil {
				t.Fatal(err)
			}
			_, err = loadInfraBuildArtifacts(dir)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("manifest error = %v, want %q", err, testCase.want)
			}
		})
	}
}

func TestInfraEnsureWithArtifactsRequiresPrebuiltSelection(t *testing.T) {
	dir := t.TempDir()
	manifest := InfraBuildManifest{Version: infraBuildManifestVersion, Artifacts: []InfraBuildArtifact{
		{Name: "libaws-test-unused-artifact", File: "libaws-test-unused-artifact/bootstrap", Sha256: strings.Repeat("a", 64)},
	}}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, infraBuildManifestFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
	infraSet := &InfraSet{Lambda: map[string]*InfraLambda{
		"libaws-test-artifact-python": {Entrypoint: "main.py"},
	}}
	err = InfraEnsureWithArtifacts(context.Background(), infraSet, "", true, false, dir)
	if err == nil || !strings.Contains(err.Error(), "requires a prebuilt artifact") {
		t.Fatalf("artifact-free selection error = %v", err)
	}
}

func TestValidateInfraBuildMode(t *testing.T) {
	if err := ValidateInfraBuildMode("dir", "other", false); err == nil {
		t.Fatal("--build-only with --from-build was accepted")
	}
	if err := ValidateInfraBuildMode("dir", "", true); err == nil {
		t.Fatal("--build-only with --preview was accepted")
	}
	if err := ValidateInfraBuildMode("", "dir", true); err != nil {
		t.Fatalf("--from-build with --preview was rejected: %v", err)
	}
	if err := ValidateInfraBuildMode("", "", false); err != nil {
		t.Fatalf("no build mode was rejected: %v", err)
	}
}

func TestInfraBuildOnlyHonorsQuickSelection(t *testing.T) {
	firstSource, firstEntrypoint := writeTestGoLambda(t, "artifact-fixture")
	secondSource, secondEntrypoint := writeTestGoLambda(t, "artifact-fixture")
	commitTestRepo(t, firstSource)
	commitTestRepo(t, secondSource)
	first := "libaws-test-build-only-quick-first"
	second := "libaws-test-build-only-quick-second"
	t.Setenv("LDFLAGS", " ")
	infraSet := &InfraSet{Lambda: map[string]*InfraLambda{
		first:  {Entrypoint: firstEntrypoint},
		second: {Entrypoint: secondEntrypoint},
	}}
	if err := InfraBuildOnly(infraSet, "", filepath.Join(t.TempDir(), "all")); err != nil {
		t.Fatalf("build all artifacts: %v", err)
	}
	quickDir := filepath.Join(t.TempDir(), "quick")
	if err := InfraBuildOnly(infraSet, second, quickDir); err != nil {
		t.Fatalf("build quick artifact: %v", err)
	}
	manifest := readBuildManifest(t, quickDir)
	if len(manifest.Artifacts) != 1 || manifest.Artifacts[0].Name != second {
		t.Fatalf("quick build artifacts = %+v", manifest.Artifacts)
	}
	err := InfraBuildOnly(infraSet, "libaws-test-build-only-quick-unknown", quickDir)
	if err == nil || !strings.Contains(err.Error(), "unknown lambda name") {
		t.Fatalf("unknown quick name error = %v", err)
	}
}
