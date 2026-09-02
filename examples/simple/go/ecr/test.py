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
    repository = f"test-ecr-{uid}"
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
                        "trigger": [{"type": "ecr"}],
                    }
                }
            }
        }
    }
    assert infra == expected, infra
    run(f"libaws ecr-ensure {repository}")
    run("libaws ecr-login")
    run("docker pull alpine:latest")
    run(f"docker tag alpine:latest $(libaws ecr-url)/{repository}:{uid}")
    run(f"docker push $(libaws ecr-url)/{repository}:{uid}")
    assert uid in run(f"libaws logs-tail /aws/lambda/test-lambda-{uid} --from-hours 1 --exit-after {uid} | tail -n1")
    run("docker logout $(libaws ecr-url)")
    run(f"docker image rm $(libaws ecr-url)/{repository}:{uid}")
    run(
        "LIBAWS_INTEGRATION=1 "
        f"LIBAWS_LAMBDA_DELETE_TEST_FUNCTION=test-lambda-{uid} "
        "go test ../../../../lib -run '^TestLambdaManualDeleteIntegration$' -count=1 -v"
    )
    run("libaws infra-rm infra.yaml --preview")
    run(f"libaws ecr-rm {repository}")
    run("libaws infra-rm infra.yaml")
    infra = yaml.safe_load(run(f"libaws infra-ls --env-values {uid}"))
    assert infra["infraset"] == {"none": None}, infra
    assert f"test-lambda-{uid}" not in run("libaws events-ls-rules")


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
