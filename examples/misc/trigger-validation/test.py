from contextlib import ExitStack
import json
import os
from pathlib import Path
import subprocess
import uuid

DIRECTORY = Path(__file__).parent


def run(*args):
    print("$ libaws", *args, flush=True)
    return subprocess.check_output(["libaws", *args], text=True, cwd=DIRECTORY).strip()


def test():
    assert run("aws-account") == os.environ["LIBAWS_TEST_ACCOUNT"]
    os.environ["uid"] = uid = uuid.uuid4().hex[:12]
    bucket, function = f"test-trigger-source-{uid}", f"test-trigger-function-{uid}"

    def verify_absent():
        assert bucket not in run("s3-ls").splitlines()
        result = subprocess.run(["libaws", "lambda-describe", function], capture_output=True, text=True)
        assert result.returncode != 0 and "ResourceNotFoundException" in result.stderr, result

    with ExitStack() as cleanup:
        cleanup.callback(verify_absent)
        cleanup.callback(run, "s3-rm-bucket", bucket)
        cleanup.callback(run, "lambda-rm", function)
        for kind in ("s3", "sqs"):
            os.environ["trigger_type"] = kind
            attributes = [[], [""], ["batch=1"]]
            if kind == "s3":
                attributes.append([bucket, "ignored-extra"])
            for attrs in attributes:
                os.environ["trigger_attributes"] = json.dumps(attrs)
                for options in ([], ["--preview"]):
                    result = subprocess.run(["libaws", "infra-ensure", "infra.yaml", *options], cwd=DIRECTORY, capture_output=True, text=True)
                    assert result.returncode != 0 and f"{kind} trigger requires" in result.stderr, result
                    assert "panic:" not in result.stderr, result
                    verify_absent()  # Real provider checks before teardown, not tag-based absence.


if __name__ == "__main__":
    test()
