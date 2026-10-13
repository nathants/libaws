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
        cases = []
        for kind in ("s3", "sqs"):
            attributes = [[], [""], ["batch=1"]]
            if kind == "s3":
                attributes.append([bucket, "ignored-extra"])
            cases.extend((kind, attrs, f"{kind} trigger requires") for attrs in attributes)
        cases.extend(("dynamodb", ["table", "start=latest", f"on-failure={destination}"], "DynamoDB failure destination") for destination in (
            "", "bucket", "arn:aws:sqs:us-east-1:123456789012:failures", "arn:aws:s3:::bucket/prefix",
        ))
        cases.append(("dynamodb", ["table", "start=latest", "metrics=unsupported"], "metrics must be event_count"))
        cases.append(("alarm", ["name=invalid", f"lambda-destination-delivery-failures={function}", "dynamodb-table=table", "at-least=1/minute"], "table scope"))
        cases.append(("alarm", ["name=invalid", f"lambda-retained-failures={function}", "dynamodb-table=table", "at-least=1/minute"], "requires a DynamoDB trigger"))
        for kind, attrs, expected_error in cases:
            os.environ["trigger_type"] = kind
            os.environ["trigger_attributes"] = json.dumps(attrs)
            for options in ([], ["--preview"]):
                result = subprocess.run(["libaws", "infra-ensure", "infra.yaml", *options], cwd=DIRECTORY, capture_output=True, text=True)
                assert result.returncode != 0 and expected_error in result.stderr, result
                assert "panic:" not in result.stderr, result
                verify_absent()


if __name__ == "__main__":
    test()
