package lib

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLambdaPythonRequirementsRelativeToInfra(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("VIRTUALENV_DOWNLOAD", "false")
	t.Setenv("VIRTUALENV_NO_PERIODIC_UPDATE", "true")
	t.Setenv("PIP_NO_INDEX", "true")
	t.Setenv("PIP_DISABLE_PIP_VERSION_CHECK", "true")
	source := t.TempDir()
	if err := os.Mkdir(filepath.Join(source, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"infra.yaml":       "name: test-requirements\nlambda:\n  test-requirements:\n    entrypoint: src/main.py\n    require:\n      - -rrequirements.txt\n",
		"src/main.py":      "def main(event, context):\n    return 'ok'\n",
		"requirements.txt": "# No dependencies: this test must not download packages.\n",
	} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	conflicting := t.TempDir()
	if err := os.WriteFile(filepath.Join(conflicting, "requirements.txt"), []byte("--invalid-caller-requirement\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, cwd string }{
		{"beside_infra", source},
		{"outside_infra", t.TempDir()},
		{"conflicting_caller_requirements", conflicting},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Chdir(test.cwd)
			infra, err := InfraParse(filepath.Join(source, "infra.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			function := infra.Lambda["test-requirements"]
			function.Name = "test-requirements"
			if err := lambdaCreateZipPy(function); err != nil {
				t.Fatalf("build with requirements next to infra: %v", err)
			}
		})
	}
}
