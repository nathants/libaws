# type: ignore
import pytest
import sys
import uuid
import shell
import yaml
import json
import os

run = lambda *a, **kw: shell.run(*a, stream=True, **kw)


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
                    "dynamodb": {
                        f"test-other-table-{uid}": {"key": ["userid:s:hash"]},
                        f"test-table-{uid}": {"attr": ["stream=keys_only"],
                                              "key": ["userid:s:hash",
                                                      "version:n:range"]},
                    },
                    "lambda": {
                        f"test-lambda-{uid}": {
                            "allow": [f"dynamodb:GetItem arn:aws:dynamodb:*:*:table/test-table-{uid}",
                                      f"dynamodb:PutItem arn:aws:dynamodb:*:*:table/test-other-table-{uid}"],
                            "attr": ["timeout=60"],
                            "policy": ["AWSLambdaDynamoDBExecutionRole",
                                       "AWSLambdaBasicExecutionRole"],
                            "trigger": [{"attr": [f"test-table-{uid}",
                                                  "batch=1",
                                                  "parallel=10",
                                                  "retry=3",
                                                  "start=trim_horizon",
                                                  "window=1"],
                                         "type": "dynamodb"}],
                            "env": [f"uid={uid}"],
                        }
                    },
                }
            }
        }
        assert infra == expected, infra
        run(f"libaws dynamodb-item-put test-table-{uid} userid:s:jane version:n:1 data:s:{uid}")
        run(f'timeout --kill-after=5s 180 libaws logs-tail /aws/lambda/test-lambda-{uid} --from-hours 1 --exit-after "dynamodb record copied"')
        assert uid == json.loads(run(f"libaws dynamodb-item-get test-other-table-{uid} userid:s:jane"))["data"]
        run(f"LIBAWS_DYNAMODB_EXAMPLE_UID={uid} go test ../../../../lib -run '^TestDynamoDBExampleFailures$' -count=1 -v")
        run("libaws infra-rm infra.yaml --preview")
    finally:
        run("libaws infra-rm infra.yaml")
    infra = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
    assert infra.get("infraset", {}) == {}, infra


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
