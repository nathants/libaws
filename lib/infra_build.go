package lib

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Two-phase Lambda deployments build artifacts on a host with the source tree
// and toolchain (--build-only, no AWS access) and deploy those exact bytes
// elsewhere (--from-build, which never compiles or reads Lambda entrypoint
// sources).
const (
	infraBuildManifestFile    = "manifest.json"
	infraBuildManifestVersion = 1
	infraBuildBootstrapName   = "bootstrap"
)

var infraBuildSha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type InfraBuildManifest struct {
	Version   int                  `json:"version"`
	Artifacts []InfraBuildArtifact `json:"artifacts"`
}

type InfraBuildArtifact struct {
	Name      string `json:"name"`
	File      string `json:"file"`
	Sha256    string `json:"sha256"`
	GitCommit string `json:"git_commit"`
	GitDirty  bool   `json:"git_dirty"`
}

func ValidateInfraBuildMode(buildOnlyDir, fromBuildDir string, preview bool) error {
	if buildOnlyDir != "" && fromBuildDir != "" {
		return errors.New("--build-only and --from-build cannot be combined")
	}
	if buildOnlyDir != "" && preview {
		return errors.New("--build-only never accesses AWS; remove --preview")
	}
	return nil
}

func validateInfraBuildArtifact(artifact InfraBuildArtifact) error {
	if err := validateLambdaName(artifact.Name); err != nil {
		return fmt.Errorf("build manifest artifact name: %w", err)
	}
	file := filepath.FromSlash(artifact.File)
	if artifact.File == "" || filepath.IsAbs(file) || !filepath.IsLocal(file) || file != filepath.Clean(file) {
		return fmt.Errorf("build manifest artifact file must be a clean relative path, got: %s", artifact.File)
	}
	if !infraBuildSha256Pattern.MatchString(artifact.Sha256) {
		return fmt.Errorf("build manifest artifact sha256 must be 64 lowercase hexadecimal characters, got: %s", artifact.Sha256)
	}
	return nil
}

type infraBuildArtifacts struct {
	dir     string
	entries map[string]InfraBuildArtifact
}

func loadInfraBuildArtifacts(dir string) (*infraBuildArtifacts, error) {
	if dir == "" {
		return nil, errors.New("build directory is empty")
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("inspect build directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("build directory is not a directory: %s", root)
	}
	data, err := os.ReadFile(filepath.Join(root, infraBuildManifestFile))
	if err != nil {
		return nil, fmt.Errorf("read build manifest: %w", err)
	}
	manifest := InfraBuildManifest{}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse build manifest: %w", err)
	}
	if manifest.Version != infraBuildManifestVersion {
		return nil, fmt.Errorf("build manifest version must be %d, got: %d", infraBuildManifestVersion, manifest.Version)
	}
	if len(manifest.Artifacts) == 0 {
		return nil, errors.New("build manifest lists no artifacts")
	}
	artifacts := &infraBuildArtifacts{
		dir:     root,
		entries: map[string]InfraBuildArtifact{},
	}
	for _, artifact := range manifest.Artifacts {
		if err := validateInfraBuildArtifact(artifact); err != nil {
			return nil, err
		}
		if _, ok := artifacts.entries[artifact.Name]; ok {
			return nil, fmt.Errorf("build manifest lists duplicate artifact name: %s", artifact.Name)
		}
		artifacts.entries[artifact.Name] = artifact
	}
	return artifacts, nil
}

// InfraBuildOnly builds every Lambda artifact the selected infrastructure set
// requires into buildDir and writes their hash manifest. It never accesses AWS.
func InfraBuildOnly(infraSet *InfraSet, quick, buildDir string) error {
	if err := validateLambdaSelection(infraSet, quick); err != nil {
		Logger.Println("error:", err)
		return err
	}
	if buildDir == "" {
		err := errors.New("build directory is empty")
		Logger.Println("error:", err)
		return err
	}
	root, err := filepath.Abs(buildDir)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if err := verifyBuildDir(root); err != nil {
		Logger.Println("error:", err)
		return err
	}
	// A stale manifest must never survive a failed build.
	if err := os.Remove(filepath.Join(root, infraBuildManifestFile)); err != nil && !os.IsNotExist(err) {
		Logger.Println("error:", err)
		return err
	}
	var names []string
	for lambdaName := range infraSet.Lambda {
		if quick != "" && quick != lambdaName {
			continue
		}
		names = append(names, lambdaName)
	}
	sort.Strings(names)
	manifest := InfraBuildManifest{Version: infraBuildManifestVersion}
	for _, lambdaName := range names {
		infraLambda := infraSet.Lambda[lambdaName]
		runtime, _, err := classifyLambdaEntrypoint(infraLambda.Entrypoint)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		if runtime != lambdaRuntimeGo {
			continue
		}
		artifactDir := filepath.Join(root, lambdaName)
		if err := os.MkdirAll(artifactDir, 0o700); err != nil {
			Logger.Println("error:", err)
			return err
		}
		bootstrap := filepath.Join(artifactDir, infraBuildBootstrapName)
		if err := lambdaBuildGoBootstrap(infraLambda, bootstrap); err != nil {
			Logger.Println("error:", err)
			return err
		}
		hash, err := sha256File(bootstrap)
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		commit, dirty, err := gitSourceIdentity(filepath.Dir(infraLambda.Entrypoint))
		if err != nil {
			Logger.Println("error:", err)
			return err
		}
		manifest.Artifacts = append(manifest.Artifacts, InfraBuildArtifact{
			Name:      lambdaName,
			File:      lambdaName + "/" + infraBuildBootstrapName,
			Sha256:    hash,
			GitCommit: commit,
			GitDirty:  dirty,
		})
		Logger.Println("built artifact:", lambdaName, hash)
	}
	if len(manifest.Artifacts) == 0 {
		err := errors.New("no Lambda artifacts to build for the selected infrastructure set")
		Logger.Println("error:", err)
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	data = append(data, '\n')
	manifestPath := filepath.Join(root, infraBuildManifestFile)
	if err := os.WriteFile(manifestPath, data, 0o600); err != nil {
		Logger.Println("error:", err)
		return err
	}
	Logger.Println("wrote build manifest:", manifestPath)
	return nil
}

// InfraEnsureWithArtifacts deploys the selected infrastructure set using only
// the hash-verified artifacts recorded in buildDir's manifest. It never builds.
func InfraEnsureWithArtifacts(ctx context.Context, infraSet *InfraSet, quick string, preview, showEnvVarValues bool, buildDir string) error {
	artifacts, err := loadInfraBuildArtifacts(buildDir)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	if err := artifacts.verifySelection(infraSet, quick); err != nil {
		Logger.Println("error:", err)
		return err
	}
	if err := infraEnsure(ctx, infraSet, quick, preview, showEnvVarValues, artifacts); err != nil {
		return err
	}
	return nil
}

// verifySelection hash-verifies every selected artifact before any provider
// call, and requires at least one selected Go Lambda.
func (artifacts *infraBuildArtifacts) verifySelection(infraSet *InfraSet, quick string) error {
	selected := 0
	for lambdaName, infraLambda := range infraSet.Lambda {
		if quick != "" && quick != lambdaName {
			continue
		}
		runtime, _, err := classifyLambdaEntrypoint(infraLambda.Entrypoint)
		if err != nil {
			return err
		}
		if runtime != lambdaRuntimeGo {
			continue
		}
		if _, err := artifacts.verifiedPath(lambdaName); err != nil {
			return err
		}
		selected++
	}
	if selected == 0 {
		return errors.New("no Lambda in the selected infrastructure set requires a prebuilt artifact")
	}
	return nil
}

func (artifacts *infraBuildArtifacts) verifiedPath(name string) (string, error) {
	artifact, ok := artifacts.entries[name]
	if !ok {
		return "", fmt.Errorf("build manifest has no artifact for Lambda: %s", name)
	}
	source := filepath.Join(artifacts.dir, filepath.FromSlash(artifact.File))
	info, err := os.Lstat(source)
	if err != nil {
		return "", fmt.Errorf("inspect Lambda artifact: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("lambda artifact is not a regular file: %s", source)
	}
	hash, err := sha256File(source)
	if err != nil {
		return "", fmt.Errorf("hash Lambda artifact: %w", err)
	}
	if hash != artifact.Sha256 {
		return "", fmt.Errorf("lambda artifact sha256 mismatch: manifest %s, file %s: %s", artifact.Sha256, hash, source)
	}
	return source, nil
}

func (artifacts *infraBuildArtifacts) packageGoLambda(infraLambda *InfraLambda) error {
	source, err := artifacts.verifiedPath(infraLambda.Name)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	dir, err := resetLambdaPackageDir(infraLambda.Name)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	bootstrap := filepath.Join(dir, infraBuildBootstrapName)
	if err := copyRegularFile(source, bootstrap); err != nil {
		Logger.Println("error:", err)
		return err
	}
	zipFile := LambdaZipFile(infraLambda.Name)
	err = shellAt(dir, "zip -0 %s ./%s", zipFile, infraBuildBootstrapName)
	if err != nil {
		Logger.Println("error:", err)
		return err
	}
	return nil
}

// verifyBuildDir proves buildDir is or becomes a writable directory before any
// artifact is written into it.
func verifyBuildDir(dir string) error {
	if info, err := os.Stat(dir); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("build directory is not a directory: %s", dir)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect build directory: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create build directory: %w", err)
	}
	probe, err := os.CreateTemp(dir, ".libaws-build-probe-*")
	if err != nil {
		return fmt.Errorf("build directory is not writable: %s: %w", dir, err)
	}
	path := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("build directory is not writable: %s: %w", dir, err)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("build directory is not writable: %s: %w", dir, err)
	}
	return nil
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func copyRegularFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	if err := output.Chmod(0o755); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}

func runCommandOutput(dir string, name string, arguments ...string) (string, error) {
	command := exec.Command(name, arguments...)
	command.Dir = dir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(arguments, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func gitSourceIdentity(dir string) (string, bool, error) {
	commit, err := runCommandOutput(dir, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", false, fmt.Errorf("resolve source commit for %s: %w", dir, err)
	}
	status, err := runCommandOutput(dir, "git", "status", "--porcelain")
	if err != nil {
		return "", false, fmt.Errorf("resolve source dirty state for %s: %w", dir, err)
	}
	return strings.TrimSpace(commit), strings.TrimSpace(status) != "", nil
}
