# type: ignore
import json
import pytest
import sys
import uuid
import shell
import yaml
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
    infra = yaml.safe_load(run(f"libaws infra-ls --env-values {uid}"))
    assert infra["infraset"] == {"none": None}, infra
    run("libaws infra-ensure infra.yaml --preview")
    run("libaws infra-ensure infra.yaml")
    infra = yaml.safe_load(run(f"libaws infra-ls --env-values {uid}"))
    infra.pop("region")
    infra.pop("account")
    infra["infraset"].pop("none")
    infra["infraset"][f"test-infraset-{uid}"].pop("keypair", None)
    expected = {
        "infraset": {
            f"test-infraset-{uid}": {
                "lambda": {
                    f"test-lambda-{uid}": {
                        "attr": ["timeout=60"],
                        "policy": ["AWSLambdaBasicExecutionRole"],
                        "trigger": [
                            {"attr": ["rate(1 minute)"], "type": "schedule"},
                            {"attr": ["cron(0 12 * * ? *)"], "type": "schedule"},
                        ],
                        "env": [f"uid={uid}"],
                    }
                }
            }
        }
    }
    assert infra == expected, infra
    assert_source_account_permissions(f"test-lambda-{uid}", "events.amazonaws.com", count=2)
    assert uid == run(f"libaws logs-tail /aws/lambda/test-lambda-{uid} --from-hours 1 --exit-after {uid} | tail -n1").split()[-1]
    run(
        "LIBAWS_INTEGRATION=1 "
        f"LIBAWS_LAMBDA_DELETE_TEST_FUNCTION=test-lambda-{uid} "
        "go test ../../../../lib -run '^TestLambdaManualDeleteIntegration$' -count=1 -v"
    )
    run("libaws infra-rm infra.yaml --preview")
    run("libaws infra-rm infra.yaml")
    infra = yaml.safe_load(run(f"libaws infra-ls --env-values {uid}"))
    assert infra["infraset"] == {"none": None}, infra
    assert f"test-lambda-{uid}" not in run("libaws events-ls-rules")


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
