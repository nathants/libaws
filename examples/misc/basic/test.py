# type: ignore
import os
import sys
import uuid

import pytest
import shell
import yaml

run = lambda *a, **kw: shell.run(*a, stream=True, **kw)


def test():
    assert os.environ["LIBAWS_TEST_ACCOUNT"] == run("libaws aws-account")
    infile = run("mktemp")
    os.environ["uid"] = uid = str(uuid.uuid4())[-12:]
    try:
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
                            "policy": ["AWSLambdaBasicExecutionRole"],
                            "env": [f"uid={uid}"],
                        }
                    }
                }
            }
        }
        assert infra == expected, infra
        run(
            "LIBAWS_INTEGRATION=1 "
            f"LIBAWS_LAMBDA_CONCURRENCY_TEST_FUNCTION=test-lambda-{uid} "
            "go test ../../../lib -run '^TestLambdaConcurrencyIntegration$' -count=1 -v"
        )
        run(f"cat - > {infile}", stdin='{"foo": "bar"}')
        assert '"foo=>bar"' == run(
            f"libaws lambda-invoke test-lambda-{uid} --payload-file {infile}"
        )
        assert uid == run(
            f"libaws logs-tail /aws/lambda/test-lambda-{uid} --from-hours 1 --exit-after {uid} | tail -n1"
        ).split()[-1]
        run("libaws infra-rm infra.yaml --preview")
    finally:
        run("rm -f", infile)
        run("libaws infra-rm infra.yaml")

    infra = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
    assert infra.get("infraset", {}) == {}, infra


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
