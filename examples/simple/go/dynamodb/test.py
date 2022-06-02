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
    infra = yaml.safe_load(run("libaws infra-ls --env-values"))
    assert sorted(infra["infraset"].keys()) == ["none"], infra
    assert sorted(infra["infraset"]["none"].keys()) == ["user"], infra
    run("libaws infra-ensure infra.yaml --preview")
    run("libaws infra-ensure infra.yaml")
    infra = yaml.safe_load(run("libaws infra-ls --env-values"))
    infra.pop("region")
    infra.pop("account")
    infra["infraset"].pop("none")
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
                                              "retry=0",
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
    run(f'libaws logs-tail /aws/lambda/test-lambda-{uid} --from-hours 1 --exit-after "put:"')
    assert uid == json.loads(run(f"libaws dynamodb-item-get test-other-table-{uid} userid:s:jane"))["data"]
    run("libaws infra-rm infra.yaml --preview")
    run("libaws infra-rm infra.yaml")
    infra = yaml.safe_load(run("libaws infra-ls --env-values"))
    assert sorted(infra["infraset"].keys()) == ["none"], infra
    assert sorted(infra["infraset"]["none"].keys()) == ["user"], infra


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
