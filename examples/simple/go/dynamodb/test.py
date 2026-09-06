# type: ignore
import pytest
import subprocess
import sys
import uuid
import shell
import yaml
import json
import os

run = lambda *a, **kw: shell.run(*a, stream=True, **kw)


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
        result = subprocess.run(["libaws", "infra-ensure", "preview.yaml", "--preview"], capture_output=True, text=True)
        assert result.returncode != 0 and "cannot update StartingPosition" in result.stderr, result
        assert f"test-new-table-{uid}" not in run("libaws dynamodb-ls").splitlines()
        run(f"libaws dynamodb-item-put test-table-{uid} userid:s:jane version:n:1 data:s:{uid}")
        run(f'libaws logs-tail /aws/lambda/test-lambda-{uid} --from-hours 1 --exit-after "put:"')
        assert uid == json.loads(run(f"libaws dynamodb-item-get test-other-table-{uid} userid:s:jane"))["data"]
        preview = captured("libaws infra-rm infra.yaml --preview")
        os.environ["LIBAWS_LAMBDA_DYNAMODB_TEST_TABLE"] = f"test-table-{uid}"
        os.environ["LIBAWS_LAMBDA_DYNAMODB_TEST_OUTPUT_TABLE"] = f"test-other-table-{uid}"
        os.environ["LIBAWS_LAMBDA_DYNAMODB_TEST_FUNCTION"] = f"test-lambda-{uid}"
        run("go test ../../../../lib -run '^TestLambdaDynamoDBStreamMappingIntegration$' -count=1 -v")
    finally:
        run("libaws infra-rm infra.yaml")
    assert "deleted trigger:" in preview, preview
    infra = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
    assert infra.get("infraset", {}) == {}, infra


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
