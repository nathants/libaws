# type: ignore
import pytest
import sys
import uuid
import shell
import yaml
import os

run = lambda *a, **kw: shell.run(*a, stream=True, **kw)


def test():
    assert os.environ["LIBAWS_TEST_ACCOUNT"] == run("libaws aws-account")
    os.environ['uid'] = uid = str(uuid.uuid4())[-12:]
    infra = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
    assert infra.get("infraset", {}) == {}, infra
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
                        "policy": [
                            "AWSLambdaSQSQueueExecutionRole",
                            "AWSLambdaBasicExecutionRole",
                        ],
                        "trigger": [
                            {
                                "attr": [
                                    f"test-queue-{uid}",
                                    "batch=1",
                                    "window=1",
                                ],
                                "type": "sqs",
                            }
                        ],
                    }
                },
                "sqs": {
                    f"test-queue-{uid}": {"attr": ["VisibilityTimeout=60"]}
                },
            }
        }
    }
    assert infra == expected, infra
    run(f"libaws sqs-send test-queue-{uid} {uid}")
    assert uid == run(f'libaws logs-tail /aws/lambda/test-lambda-{uid} --from-hours 1 --exit-after "thanks for:" | tail -n1').split()[-1]
    run("libaws infra-rm infra.yaml --preview")
    run("libaws infra-rm infra.yaml")
    infra = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
    assert infra.get("infraset", {}) == {}, infra


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
