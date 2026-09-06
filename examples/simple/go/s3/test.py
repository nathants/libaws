# type: ignore
import json
import pytest
import subprocess
import sys
import uuid
import shell
import yaml
import os

run = lambda *a, **kw: shell.run(*a, stream=True, **kw)


def assert_source_account_permission(function_name, service):
    policy = json.loads(run(f"libaws lambda-permissions {function_name}"))
    statements = [
        statement
        for statement in policy["Statement"]
        if statement.get("Principal", {}).get("Service") == service
    ]
    assert len(statements) == 1, statements
    assert statements[0]["Condition"]["StringEquals"]["AWS:SourceAccount"] == os.environ["LIBAWS_TEST_ACCOUNT"]
    assert statements[0]["Condition"]["ArnLike"]["AWS:SourceArn"]


def captured(command):
    result = subprocess.run(command, shell=True, text=True, capture_output=True)
    sys.stdout.write(result.stdout)
    sys.stderr.write(result.stderr)
    assert result.returncode == 0, result
    return result.stdout + result.stderr


def test():
    assert os.environ["LIBAWS_TEST_ACCOUNT"] == run("libaws aws-account")
    os.environ['uid'] = uid = str(uuid.uuid4())[-12:]
    infra = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
    assert infra.get("infraset", {}) == {}, infra
    try:
        run("libaws infra-ensure infra.yaml --preview")
        run("libaws infra-ensure infra.yaml")
        infra = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
        infra.pop("region")
        infra.pop("account")
        infra["infraset"][f"test-infraset-{uid}"].pop("keypair", None)
        expected = {
            "infraset": {
                f"test-infraset-{uid}": {
                    "lambda": {
                        f"test-lambda-{uid}": {
                            "attr": ["timeout=60"],
                            "policy": ["AWSLambdaBasicExecutionRole"],
                            "trigger": [{"attr": [f"test-bucket-{uid}"],
                                         "type": "s3"}],
                        }
                    },
                    "s3": {f"test-bucket-{uid}": {'attr': ['acl=private']}},
                }
            }
        }
        assert infra == expected, infra
        assert_source_account_permission(f"test-lambda-{uid}", "s3.amazonaws.com")
        run(f"echo | libaws s3-put s3://test-bucket-{uid}/{uid}")
        assert uid == run(f"libaws logs-tail /aws/lambda/test-lambda-{uid} --from-hours 1 --exit-after {uid} | tail -n1").split()[-1]
        run(
            "LIBAWS_INTEGRATION=1 "
            f"LIBAWS_LAMBDA_DELETE_TEST_FUNCTION=test-lambda-{uid} "
            "go test ../../../../lib -run '^TestLambdaManualDeleteIntegration$' -count=1 -v"
        )
        preview = captured("libaws infra-rm infra.yaml --preview")
    finally:
        run("libaws infra-rm infra.yaml")
    assert "deleted bucket notification:" in preview, preview
    infra = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
    assert infra.get("infraset", {}) == {}, infra


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
