from contextlib import ExitStack
import json
import os
from pathlib import Path
import re
import subprocess
import uuid

ROOT = Path(__file__).resolve().parents[3]
LIBAWS = str(ROOT / "libaws")


def test():
    account = os.environ.get("LIBAWS_R2_TEST_ACCOUNT")
    if not account:
        print("SKIP R2: set LIBAWS_R2_TEST_ACCOUNT and explicit R2 credentials to authorize a live fixture", flush=True)
        return
    assert re.fullmatch(r"[0-9a-f]{32}", account) and account == os.environ["R2_ACCOUNT_ID"], "R2 account guard failed"
    assert os.environ["R2_ACCESS_KEY_ID"] and os.environ["R2_ACCESS_KEY_SECRET"]
    bucket = "libaws-testing-" + uuid.uuid4().hex
    # A mistaken fallback to ordinary AWS credentials must not succeed.
    env = dict(os.environ, AWS_ACCESS_KEY_ID="must-not-use-aws", AWS_SECRET_ACCESS_KEY="must-not-use-aws", AWS_SESSION_TOKEN="", AWS_REGION="us-west-2", LIBAWS_R2_TEST_BUCKET=bucket)

    def fixture(step):
        print(f"R2 fixture {step}: {bucket}", flush=True)
        subprocess.run(["go", "test", "./lib", "-run", "^TestR2ExampleFixture$", "-count=1", "-v"], cwd=ROOT, env=dict(env, LIBAWS_R2_TEST_STEP=step), check=True)

    def run(command, path, *args, input=None):
        print(f"$ libaws {command} {path} --r2", *args, flush=True)
        return subprocess.check_output([LIBAWS, command, path, "--r2", *args], input=input, env=env, timeout=60)

    # Prove absence before registering destructive cleanup. The name is unique;
    # cleanup never enumerates or modifies other buckets, domains, or tokens.
    fixture("absent")
    with ExitStack() as cleanup:
        cleanup.callback(fixture, "absent")
        cleanup.callback(fixture, "cleanup")
        fixture("create")
        objects = {"plain.txt": b"first\n", "nested/data.bin": bytes(range(256))}
        for key, value in objects.items():
            path = f"s3://{bucket}/{key}"
            run("s3-put", path, input=value)
            assert run("s3-get", path) == value
            head = json.loads(run("s3-head", path))
            assert head["ContentLength"] == len(value), head
        listed = run("s3-ls", bucket + "/", "--recursive", "--quiet").decode().splitlines()
        assert listed == [f"{bucket}/{key}" for key in sorted(objects)], listed
        first = f"{bucket}/plain.txt"
        run("s3-rm", first, "--preview")
        assert run("s3-get", first) == objects["plain.txt"]
        run("s3-rm", first)
        listed = run("s3-ls", bucket + "/", "--recursive", "--quiet").decode().splitlines()
        assert listed == [f"{bucket}/nested/data.bin"], listed
        # Leave one object to exercise independent SDK cleanup of a nonempty bucket.


if __name__ == "__main__":
    test()
