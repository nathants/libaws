# type: ignore
import json
import uuid
import pytest
import sys
import shell
import yaml
import time
import os

run = lambda *a, **kw: shell.run(*a, stream=True, **kw)


def assert_source_account_permissions(function_name, service, count=1):
    policy = json.loads(run(f"libaws lambda-permissions {function_name}"))
    statements = [
        statement
        for statement in policy["Statement"]
        if statement.get("Principal", {}).get("Service") == service
    ]
    assert len(statements) == count, statements
    assert {
        statement["Condition"]["StringEquals"]["AWS:SourceAccount"]
        for statement in statements
    } == {os.environ["LIBAWS_TEST_ACCOUNT"]}
    assert len(
        {
            statement["Condition"]["ArnLike"]["AWS:SourceArn"]
            for statement in statements
        }
    ) == count


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
        infra["infraset"][f"test-infraset-{uid}"]["lambda"][f"test-lambda-{uid}"]['trigger'][1].pop("attr")
        expected = {
            "infraset": {
                f"test-infraset-{uid}": {
                    "lambda": {
                        f"test-lambda-{uid}": {
                            "attr": ["timeout=60"],
                            "policy": ["AWSLambdaBasicExecutionRole"],
                            "trigger": [
                                {"attr": ["rate(15 minutes)"],
                                 "type": "schedule"},
                                {"type": "api"},
                            ],
                        }
                    }
                }
            }
        }
        assert infra == expected, infra
        assert_source_account_permissions(f"test-lambda-{uid}", "apigateway.amazonaws.com")
        url = run(f"libaws infra-url-api infra.yaml test-lambda-{uid}")
        for _ in range(10):
            try:
                run(f"curl -f {url} 2>/dev/null")
            except:
                time.sleep(1)
            else:
                break
        else:
            assert False, "fail"
        assert 'hi' == run(f'curl {url} 2>/dev/null')
        run(
            "LIBAWS_INTEGRATION=1 "
            f"LIBAWS_LAMBDA_DELETE_TEST_FUNCTION=test-lambda-{uid} "
            "go test ../../../../lib -run '^TestLambdaManualDeleteIntegration$' -count=1 -v"
        )
        run(f"libaws lambda-rm test-lambda-{uid} --preview")
        run(f"libaws lambda-rm test-lambda-{uid}")
        run("libaws infra-rm infra.yaml --preview")
    finally:
        run("libaws infra-rm infra.yaml")
    infra = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
    assert infra.get("infraset", {}) == {}, infra
    assert f"test-lambda-{uid}" not in run("libaws api-ls").splitlines()


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
