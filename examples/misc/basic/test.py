# type: ignore
import json
import os
import subprocess
import sys
import uuid

import pytest
import shell
import yaml

run = lambda *a, **kw: shell.run(*a, stream=True, **kw)


def environment(value):
    # JSON quoting is also valid YAML and preserves controls and Unicode exactly.
    os.environ["lambda_environment"] = json.dumps("AA=" + value)


def reject_environment(value, message, *options):
    environment(value)
    result = subprocess.run(
        ["libaws", "infra-ensure", "infra.yaml", *options],
        capture_output=True, text=True, check=False,
    )
    assert result.returncode != 0 and message in result.stderr, result


def assert_function_absent(name):
    result = subprocess.run(
        ["libaws", "lambda-describe", name], capture_output=True, text=True, check=False,
    )
    assert result.returncode != 0 and "ResourceNotFoundException" in result.stderr, result


def test():
    assert os.environ["LIBAWS_TEST_ACCOUNT"] == run("libaws aws-account")
    infile = run("mktemp")
    os.environ["uid"] = uid = str(uuid.uuid4())[-12:]
    name = f"test-lambda-{uid}"
    # AA and the 12-byte uid occupy 30 bytes of JSON syntax/names/uid. The
    # separators add 6 UTF-8 bytes, leaving 4060 bytes at the 4 KiB quota.
    quota_value = "\u2028\u2029" + "x" * 4060
    try:
        infra = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
        assert infra.get("infraset", {}) == {}, infra
        for options in ([], ["--preview"]):
            reject_environment(quota_value + "x", "environment size 4097 bytes", *options)
            reject_environment("\b" * 800 + "x" * 231, "request size 5121 bytes", *options)
        assert_function_absent(name)
        environment(quota_value)
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
                            "env": [f"AA={quota_value}", f"uid={uid}"],
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
        run(
            f"LIBAWS_LAMBDA_ENVIRONMENT_TEST_FUNCTION={name} "
            "go test ../../../lib -run '^TestLambdaEnvironmentIntegration$' -count=1 -v"
        )
        for character, options in (("\b", []), ("\f", ["--quick", name])):
            # 5120 request bytes, including the uid entry and timeout/memory.
            value = character * 800 + "x" * 230
            environment(value)
            run("libaws infra-ensure infra.yaml", *options)
            for invalid_options in (options, [*options, "--preview"]):
                reject_environment(value + "x", "request size 5121 bytes", *invalid_options)
                reject_environment(quota_value + "x", "environment size 4097 bytes", *invalid_options)
            actual = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
            assert actual["infraset"][f"test-infraset-{uid}"]["lambda"][name]["env"] == [f"AA={value}", f"uid={uid}"]
        environment(quota_value)
        run("libaws infra-rm infra.yaml --preview")
    finally:
        environment(quota_value)
        run("rm -f", infile)
        run("libaws infra-rm infra.yaml")
        assert_function_absent(name)

    infra = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
    assert infra.get("infraset", {}) == {}, infra


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
