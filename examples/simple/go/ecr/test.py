# type: ignore
import json
import pytest
import shlex
import sys
import uuid
import shell
import yaml
import os

run = lambda *a, **kw: shell.run(*a, stream=True, **kw)


def assert_source_account_permission(function_name, service):
    policy = json.loads(run(f"libaws lambda-permissions {function_name}"))
    statements = [
        statement
        for statement in policy["Statement"]
        if statement.get("Principal", {}).get("Service") == service
    ]
    assert len(statements) == 1, statements
    assert statements[0]["Condition"]["StringEquals"]["AWS:SourceAccount"] == os.environ["LIBAWS_TEST_ACCOUNT"]
    assert statements[0]["Condition"]["ArnLike"]["AWS:SourceArn"]


def test(tmp_path):
    assert os.environ["LIBAWS_TEST_ACCOUNT"] == run("libaws aws-account")
    os.environ['uid'] = uid = str(uuid.uuid4())[-12:]
    repository = f"test-ecr-{uid}"
    source_image = f"libaws-ecr-test:{uid}"
    if not os.environ.get("DOCKER_HOST"):
        os.environ["DOCKER_HOST"] = run(
            "docker context inspect --format '{{.Endpoints.docker.Host}}'"
        )
    docker_config = tmp_path / "docker"
    docker_config.mkdir()
    os.environ["DOCKER_CONFIG"] = str(docker_config)
    image_context = tmp_path / "image"
    image_context.mkdir()
    (image_context / "marker").write_text(uid, encoding="utf-8")
    (image_context / "Dockerfile").write_text(
        f"FROM scratch\nCOPY marker /marker\nLABEL libaws-test={uid}\n",
        encoding="utf-8",
    )
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
    assert_source_account_permission(f"test-lambda-{uid}", "events.amazonaws.com")
    run(f"libaws ecr-ensure {repository}")
    run("libaws ecr-login")
    run(
        f"docker build --provenance=false -t {source_image} "
        f"{shlex.quote(str(image_context))}"
    )
    image = f"$(libaws ecr-url)/{repository}:{uid}"
    run(f"docker tag {source_image} {image}")
    run(f"docker push {image}")
    assert uid in run(f"libaws logs-tail /aws/lambda/test-lambda-{uid} --from-hours 1 --exit-after {uid} | tail -n1")
    run("docker logout $(libaws ecr-url)")
    run(f"docker image rm {image} {source_image}")
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
