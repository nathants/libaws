package lib

import (
	"os/exec"
	"testing"
)

func TestSuiteRunnerBoundsAndDrainsWorkers(t *testing.T) {
	command := exec.Command("python3", "-c", `
import threading
import time
import test_runner

lock = threading.Lock()
barrier = threading.Barrier(2, timeout=5)
active = peak = 0

def worker(label):
    global active, peak
    with lock:
        active += 1
        peak = max(peak, active)
    barrier.wait()
    time.sleep(0.01)
    with lock:
        active -= 1
    return label, 0.01, int(label == 1)

results = test_runner.run_pool(range(6), 2, worker, lambda: False)
assert peak == 2 and active == 0, (peak, active)
assert sorted(label for label, _, _ in results) == list(range(6)), results
assert sum(status for _, _, status in results) == 1, results

stopped = threading.Event()
def stopping_worker(label):
    stopped.set()
    return label
assert test_runner.run_pool(range(6), 1, stopping_worker, stopped.is_set) == [0]
assert test_runner.run_pool(range(6), 2, worker, lambda: True) == []
`)
	command.Dir = ".."
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("test runner regression: %v\n%s", err, output)
	}
}

func TestPythonRestoreResolvesLauncherBeforeCopyingInterpreter(t *testing.T) {
	command := exec.Command("python3", "-c", `
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

script = Path('restore_python_deps.sh').resolve()
real_python = Path(sys._base_executable).resolve()
with tempfile.TemporaryDirectory(prefix='libaws-restore-test-') as temporary:
    root = Path(temporary)
    launcher = root/'launcher'
    launcher.mkdir()
    (launcher/'python3').symlink_to(real_python)
    (root/'pyproject.toml').write_text('[project]\nname="restore-test"\nversion="0.0.0"\nrequires-python=">=3.12"\ndependencies=[]\n[tool.uv]\npackage=false\n')
    shutil.copyfile(script, root/'restore_python_deps.sh')
    env = os.environ.copy()
    env.update(PATH=str(launcher)+os.pathsep+env['PATH'], UV_PYTHON_DOWNLOADS='never')
    env.pop('VIRTUAL_ENV', None)
    subprocess.run(['uv', 'lock', '--offline', '--python', str(real_python)], cwd=root, env=env, check=True)
    for _ in range(2):
        subprocess.run(['bash', 'restore_python_deps.sh'], cwd=root, env=env, check=True)
        config = dict(line.split(' = ', 1) for line in (root/'.venv/pyvenv.cfg').read_text().splitlines() if ' = ' in line)
        assert Path(config['home']).resolve() == real_python.parent, (config, real_python)
        assert not (root/'.venv/bin/python').is_symlink()
        for name in ['python', 'python3', f'python{sys.version_info.major}.{sys.version_info.minor}']:
            assert not (root/'.venv/bin'/name).is_symlink(), name
            subprocess.run([str(root/'.venv/bin'/name), '-c', 'import encodings, json, ssl'], check=True)
`)
	command.Dir = ".."
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Python restore regression: %v\n%s", err, output)
	}
}
