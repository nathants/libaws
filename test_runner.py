"""Bounded runner for independent examples and isolated library test files."""
import concurrent.futures
import json
import os
from pathlib import Path
import signal
import subprocess
import tempfile
import time

ROOT = Path(__file__).resolve().parent
CLEANUP = "examples/misc/cleanup/test.py"
# These tests deliberately observe/mutate account-wide state.
EXCLUSIVE = {
    "lib/s3_test.go", "examples/simple/go/ses/test.py",
    f"{CLEANUP}::test_live_failure_cleanup[simple/go/ses]",
}


def discover():
    info = json.loads(subprocess.check_output(["go", "list", "-json", "./lib"], cwd=ROOT))
    tests = ["lib/" + name for name in info["TestGoFiles"] if name != "lib_test.go"]
    for directory, children, files in os.walk(ROOT / "examples"):
        children[:] = sorted(name for name in children if name not in (".venv", "node_modules", ".git"))
        if "test.py" in files:
            label = str((Path(directory) / "test.py").relative_to(ROOT))
            if label == CLEANUP:
                from examples.misc.cleanup.test import LIVE_EXAMPLES
                # Separate processes isolate patched environment/cwd state, and
                # the existing pool bounds total concurrency across all tests.
                tests.append(f"{label}::nonlive")
                tests.extend(f"{label}::test_live_failure_cleanup[{example}]" for example in LIVE_EXAMPLES)
            else:
                tests.append(label)
    missing = EXCLUSIVE - set(tests)
    if missing:
        raise RuntimeError(f"exclusive test entries no longer exist: {sorted(missing)}")
    return tests


def run_test(label, logs):
    started = time.monotonic()
    logfile = logs / (label.replace("/", "__") + ".log")
    print(f"START {label}  log={logfile}", flush=True)
    with tempfile.TemporaryDirectory(prefix="libaws-test-") as temporary, logfile.open("w") as output:
        env = os.environ.copy()
        env["PATH"] = str(ROOT) + os.pathsep + env["PATH"]
        source, _, selection = label.partition("::")
        if source.endswith(".go"):
            command = ["bash", str(ROOT / "test_one.sh"), Path(source).name.removesuffix("_test.go")]
            cwd = ROOT
        else:
            env["PYTEST_ADDOPTS"] = env.get("PYTEST_ADDOPTS", "") + f" --basetemp={temporary}/pytest -o cache_dir={temporary}/cache"
            command = ["timeout", "--kill-after=30s", "1800", "uv", "run", "--locked", "python", "-u"]
            if source == CLEANUP and selection:
                command += ["-m", "pytest", "-svvx", "--tb", "native"]
                if selection == "nonlive":
                    command += ["test.py", "-k", "not test_live_failure_cleanup"]
                else:
                    command += [f"test.py::{selection}"]
            else:
                command += ["test.py"]
            cwd = ROOT / Path(source).parent
        try:
            status = subprocess.call(command, cwd=cwd, env=env, stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
        except OSError as error:
            print(error, file=output)
            status = 1
    seconds = time.monotonic() - started
    print(f"{'PASS' if status == 0 else 'FAIL'} {label}  {seconds:.1f}s  status={status}", flush=True)
    if status:
        print("".join(logfile.read_text(errors="replace").splitlines(keepends=True)[-60:]), flush=True)
    return label, seconds, status


def run_pool(tests, jobs, worker, stopping):
    """Do not queue beyond the worker bound; drain started tests on cancellation."""
    pending = iter(tests)
    results = []
    with concurrent.futures.ThreadPoolExecutor(max_workers=jobs) as pool:
        running = set()
        while True:
            while not stopping() and len(running) < jobs:
                label = next(pending, None)
                if label is None:
                    break
                running.add(pool.submit(worker, label))
            if not running:
                return results
            done, running = concurrent.futures.wait(running, return_when=concurrent.futures.FIRST_COMPLETED)
            results.extend(future.result() for future in done)


def main():
    jobs = int(os.environ.get("LIBAWS_TEST_JOBS", "4"))
    if jobs < 1:
        raise ValueError("LIBAWS_TEST_JOBS must be a positive integer")
    logs = Path(tempfile.mkdtemp(prefix="libaws-tests-"))
    started = time.monotonic()
    stopped = False

    def stop(signum, frame):
        nonlocal stopped
        stopped = True
        print("Stopping new tests; waiting for running tests and their cleanup.", flush=True)

    signal.signal(signal.SIGINT, stop)
    signal.signal(signal.SIGTERM, stop)
    tests = discover()
    print(f"{len(tests)} tests; jobs={jobs}; logs={logs}", flush=True)
    results = run_pool([test for test in tests if test in EXCLUSIVE], 1, lambda test: run_test(test, logs), lambda: stopped)
    results += run_pool([test for test in tests if test not in EXCLUSIVE], jobs, lambda test: run_test(test, logs), lambda: stopped)
    with (logs / "timings.tsv").open("w") as output:
        output.write("test\tseconds\tstatus\n")
        for label, seconds, status in results:
            output.write(f"{label}\t{seconds:.3f}\t{status}\n")
    failures = [label for label, _, status in results if status]
    print(f"TOTAL tests: {time.monotonic() - started:.1f}s; completed={len(results)}/{len(tests)}; failed={len(failures)}; logs={logs}", flush=True)
    return 130 if stopped else int(bool(failures))


if __name__ == "__main__":
    raise SystemExit(main())
