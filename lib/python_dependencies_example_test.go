package lib

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPythonDependencyExampleRejectsStderrZipDrift(t *testing.T) {
	python, err := filepath.Abs("../.venv/bin/python")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(python, "-c", `
import importlib.util
import os
from pathlib import Path
import tempfile
from unittest.mock import patch
import yaml

path = Path('examples/simple/python/dependencies/test.py').resolve()
spec = importlib.util.spec_from_file_location('dependencies_example', path)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
actual_run = module.run
uid = 'abcdef123456'
reads = previews = 0
emitted = False
existing = {
    'account': 'guarded', 'region': 'us-east-1',
    'infraset': {f'test-infraset-{uid}': {'lambda': {f'test-lambda-{uid}': {
        'attr': ['timeout=60'], 'policy': ['AWSLambdaBasicExecutionRole'], 'env': [f'uid={uid}'],
    }}}},
}

def run(command, *args, **kwargs):
    global reads, previews, emitted
    if command == 'libaws aws-account':
        return 'guarded'
    if command.startswith('libaws infra-ls '):
        reads += 1
        return yaml.safe_dump(existing if reads == 2 else {'account': 'guarded', 'region': 'us-east-1'})
    if command == 'libaws' and args[0] == 'infra-ensure' and '--preview' in args:
        previews += 1
        if previews == 2:
            emitted = True
            # Real py-shell process/stream handling; only the provider command is
            # replaced. Preserve the example's capture options, cwd and raw argv.
            return actual_run('sh', '-c', "printf 'preview: zip update: main.py = changed\\n' >&2", **kwargs)
    if command.startswith('libaws logs-tail '):
        return uid
    if command.startswith('libaws lambda-describe '):
        return {'exitcode': 1, 'stderr': 'ResourceNotFoundException'}
    return ''

with tempfile.TemporaryDirectory() as temporary, patch.dict(os.environ, LIBAWS_TEST_ACCOUNT='guarded'), patch.object(module.uuid, 'uuid4', return_value=uid), patch.object(module, 'run', side_effect=run):
    try:
        module.test(Path(temporary))
    except AssertionError as error:
        assert 'preview: zip' in str(error), error
    else:
        raise AssertionError('example missed real stderr ZIP drift')
assert emitted, 'fixture did not reach convergence assertion'
`)
	command.Dir = ".."
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Python dependency convergence acceptance: %v\n%s", err, output)
	}
}
