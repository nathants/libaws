# type: ignore
import json
import uuid
import pytest
import sys
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
    assert (
        statements[0]["Condition"]["StringEquals"]["AWS:SourceAccount"]
        == os.environ["LIBAWS_TEST_ACCOUNT"]
    )
    assert statements[0]["Condition"]["ArnLike"]["AWS:SourceArn"]


def test():
    assert os.environ["LIBAWS_TEST_ACCOUNT"] == run("libaws aws-account")
    os.environ['uid'] = uid = str(uuid.uuid4())[-12:]
    infra = yaml.safe_load(run(f"libaws infra-ls --env-values {uid}"))
    assert infra["infraset"] == {"none": None}, infra
    run("libaws infra-ensure infra.yaml --preview")
    run("libaws infra-ensure infra.yaml")
    infra = yaml.safe_load(run(f"libaws infra-ls --env-values {uid}"))
    infra.pop("region")
    infra.pop("account")
    infra["infraset"].pop("none")
    infra["infraset"][f"test-infraset-{uid}"].pop("keypair", None)
    infra["infraset"][f"test-infraset-{uid}"]["lambda"][f"test-lambda-{uid}"]['trigger'][0].pop("attr")
    expected = {
        "infraset": {
            f"test-infraset-{uid}": {
                "lambda": {
                    f"test-lambda-{uid}": {
                        "attr": ["timeout=60"],
                        "policy": ["AWSLambdaBasicExecutionRole"],
                        "trigger": [{"type": "websocket"}],
                    }
                }
            }
        }
    }
    assert infra == expected, infra
    assert_source_account_permission(
        f"test-lambda-{uid}", "apigateway.amazonaws.com"
    )
    run(f"libaws infra-url-websocket infra.yaml test-lambda-{uid}")
    run(
        "LIBAWS_INTEGRATION=1 "
        f"LIBAWS_LAMBDA_DELETE_TEST_FUNCTION=test-lambda-{uid} "
        "go test ../../../../lib -run '^TestLambdaManualDeleteIntegration$' -count=1 -v"
    )
    run("libaws infra-rm infra.yaml --preview")
    run("libaws infra-rm infra.yaml")
    infra = yaml.safe_load(run(f"libaws infra-ls --env-values {uid}"))
    assert infra["infraset"] == {"none": None}, infra
    assert f"test-lambda-{uid}___websocket" not in run("libaws api-ls").splitlines()


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
