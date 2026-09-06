package lib

import (
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func TestPythonExampleDeclarationsPassBuildConstraints(t *testing.T) {
	t.Setenv("uid", "abcdef123456")
	t.Setenv("lambda_environment", "AA=fixture")
	t.Setenv("logs_ttl_days", "7")
	for _, example := range []string{"misc/basic", "simple/python/dynamodb", "simple/python/dependencies"} {
		t.Run(example, func(t *testing.T) {
			infra, err := InfraParse(filepath.Join("..", "examples", example, "infra.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			for _, function := range infra.Lambda {
				if !slices.Contains(function.Require, "--build-constraint=build-requirements.txt") {
					t.Fatalf("example passes unconstrained build requirements to pip: %v", function.Require)
				}
			}
		})
	}
}

func TestPythonBuildDependenciesAreLocked(t *testing.T) {
	command := exec.Command("python3", "-c", `
from pathlib import Path
import subprocess
import tomllib

root = Path.cwd()
projects = [root, root/'examples/misc/basic', root/'examples/simple/python/dynamodb', root/'examples/simple/python/dependencies']
for project in projects:
    metadata = tomllib.loads((project/'pyproject.toml').read_text())
    constraints = metadata['tool']['uv'].get('build-constraint-dependencies', [])
    assert constraints == ['setuptools==83.0.0'], f'{project}: unconstrained isolated build: {constraints}'
    assert metadata['dependency-groups']['build'] == constraints, project
    locked = tomllib.loads((project/'uv.lock').read_text())
    versions = {p['name']: p['version'] for p in locked['package'] if 'registry' in p.get('source', {})}
    assert versions['setuptools'] == '83.0.0', project
    exported = subprocess.check_output(['uv', 'export', '--locked', '--only-group', 'build', '--no-header', '--no-annotate', '--no-hashes'], cwd=project, text=True)
    assert exported == (project/'build-requirements.txt').read_text(), project
`)
	command.Dir = ".."
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Python build dependency metadata: %v\n%s", err, output)
	}
}
