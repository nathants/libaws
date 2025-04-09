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
    infile = run("mktemp")
    os.environ["uid"] = uid = str(uuid.uuid4())[-12:]
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
    run(f"cat - > {infile}", stdin='{"foo": "bar"}')
    assert '"foo=>bar"' == run(f"libaws lambda-invoke test-lambda-{uid} --payload-file {infile}")
    assert uid == run(f"libaws logs-tail /aws/lambda/test-lambda-{uid} --from-hours 1 --exit-after {uid} | tail -n1").split()[-1]
    run("libaws infra-rm infra.yaml --preview")
    run("rm -f", infile)
    run("libaws infra-rm infra.yaml")
    infra = yaml.safe_load(run("libaws infra-ls --env-values"))
    assert sorted(infra["infraset"].keys()) == ["none"], infra
    assert sorted(infra["infraset"]["none"].keys()) == ["user"], infra


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
