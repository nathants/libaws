"""Delete only verified AMI backing snapshots, preserving shared and unrelated data."""
from contextlib import ExitStack
import json
import os
from pathlib import Path
import subprocess
import tempfile
import uuid

ROOT = Path(__file__).resolve().parents[3]
CLI = str(ROOT / "libaws")


def run(*args, env=None):
    print("$", *args, flush=True)
    if args[:2] == ("go", "test"):
        return subprocess.run((*args, "-timeout=12m"), cwd=ROOT, env=env, check=True, timeout=900)
    result = subprocess.run(args, cwd=ROOT, env=env, capture_output=True, text=True, timeout=60)
    if result.stdout:
        print(result.stdout, end="", flush=True)
    if result.stderr:
        print(result.stderr, end="", flush=True)
    result.check_returncode()
    return result


def verify_snapshot_deletion(fixture, env):
    # The backend reports per-snapshot success/failure. A description is never
    # evidence of association, and this path must never call DeleteSnapshot.
    deleted = run(CLI, "ec2-rm-ami", fixture["target"])
    assert f"deleted backing snapshot: {fixture['exclusive']}" in deleted.stderr, deleted
    assert f"preserved shared snapshot: {fixture['shared']}" in deleted.stderr, deleted
    assert fixture["decoy"] not in deleted.stderr, deleted
    run("go", "test", "./lib", "-run", "^TestEC2AmiExampleVerify$", "-count=1", "-v", env=env)


def test():
    assert run(CLI, "aws-account").stdout.strip() == os.environ["LIBAWS_TEST_ACCOUNT"]
    uid = uuid.uuid4().hex[:12]
    with tempfile.TemporaryDirectory(prefix="libaws-ami-") as directory, ExitStack() as cleanup:
        env = dict(os.environ, LIBAWS_EC2_AMI_UID=uid, LIBAWS_EC2_AMI_DIR=directory)
        cleanup.callback(run, "go", "test", "./lib", "-run", "^TestEC2AmiExampleCleanup$", "-count=1", "-v", env=env)
        run("go", "test", "./lib", "-run", "^TestEC2AmiExamplePrepare$", "-count=1", "-v", env=env)
        fixture = json.loads((Path(directory) / "fixture.json").read_text())
        for flag in ("--preview", "-p"):
            preview = run(CLI, "ec2-rm-ami", fixture["target"], flag)
            assert not preview.stdout and f"preview: deregister: {fixture['target']}" in preview.stderr, preview
            assert fixture["exclusive"] in preview.stderr and fixture["shared"] in preview.stderr, preview
            assert fixture["decoy"] not in preview.stderr, preview
        run("go", "test", "./lib", "-run", "^TestEC2AmiExampleVerifyPreview$", "-count=1", "-v", env=env)
        verify_snapshot_deletion(fixture, env)
        # Repeating deletion after confirmed absence is a no-op.
        for flags in ([], ["--preview"]):
            absent = run(CLI, "ec2-rm-ami", fixture["target"], *flags)
            assert not absent.stdout and not absent.stderr, absent


if __name__ == "__main__":
    test()
