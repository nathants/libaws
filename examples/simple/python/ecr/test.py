# type: ignore
import pytest
import shlex
import subprocess
import sys
import uuid
import shell
import yaml
import os
from contextlib import ExitStack
from unittest.mock import patch

run = lambda *a, **kw: shell.run(*a, stream=True, **kw)


def exercise(tmp_path):
    assert os.environ["LIBAWS_TEST_ACCOUNT"] == run("libaws aws-account")
    os.environ['uid'] = uid = str(uuid.uuid4())[-12:]
    repository = f"test-{uid}"
    source_image = f"libaws-ecr-test:{uid}"
    image_context = tmp_path / "image"
    image_context.mkdir()
    (image_context / "marker").write_text(uid, encoding="utf-8")
    (image_context / "Dockerfile").write_text(
        f"FROM scratch\nCOPY marker /marker\nLABEL libaws-test={uid}\n",
        encoding="utf-8",
    )
    if not os.environ.get("DOCKER_HOST"):
        os.environ["DOCKER_HOST"] = run(
            "docker context inspect --format '{{.Endpoints.docker.Host}}'"
        )
    docker_config = tmp_path / "docker"
    docker_config.mkdir()
    os.environ["DOCKER_CONFIG"] = str(docker_config)
    infra = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
    assert infra.get("infraset", {}) == {}, infra
    with ExitStack() as cleanup:
        # Register before provisioning so partial AWS creates are also removed.
        # ExitStack runs every callback even when a previous cleanup fails.
        cleanup.callback(run, "libaws infra-rm infra.yaml")
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
                            "trigger": [{"type": "ecr"}],
                        }
                    }
                }
            }
        }
        assert infra == expected, infra
        cleanup.callback(run, f"libaws ecr-rm {repository}")
        run(f"libaws ecr-ensure {repository}")
        run("libaws ecr-login")
        run(
            f"docker build --provenance=false -t {source_image} "
            f"{shlex.quote(str(image_context))}"
        )
        cleanup.callback(run, f"docker image rm {source_image}")
        image = f"$(libaws ecr-url)/{repository}:{uid}"
        run(f"docker tag {source_image} {image}")
        cleanup.callback(run, f"docker image rm {image}")
        run(f"docker push {image}")
        assert uid in run(f"libaws logs-tail /aws/lambda/test-lambda-{uid} --from-hours 1 --exit-after {uid} | tail -n1")
        run("libaws infra-rm infra.yaml --preview")
    assert_cleanup(uid)


def assert_cleanup(uid):
    infra = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
    assert infra.get("infraset", {}) == {}, infra
    # ECR repositories are untagged, so infrastructure inventory cannot prove deletion.
    assert f"test-{uid}" not in run("libaws ecr-ls").splitlines()
    result = subprocess.run(
        ["libaws", "lambda-describe", f"test-lambda-{uid}"],
        capture_output=True, text=True, check=False,
    )
    assert result.returncode != 0 and "ResourceNotFoundException" in result.stderr, result
    registry = run("libaws ecr-url")
    for image in (f"libaws-ecr-test:{uid}", f"{registry}/test-{uid}:{uid}"):
        result = subprocess.run(
            ["docker", "image", "inspect", image], capture_output=True, text=True, check=False,
        )
        assert result.returncode != 0 and "No such image" in result.stderr, result


def test(tmp_path):
    with patch.dict(os.environ):
        exercise(tmp_path)


def test_live_cleanup_after_login_failure(tmp_path):
    actual_run = run
    uid = None
    with ExitStack() as safety_cleanup:
        def fail_login(command, *args, **kwargs):
            nonlocal uid
            if uid is None and command == "libaws infra-ensure infra.yaml":
                uid = os.environ["uid"]
                # A regression in the cleanup being tested must not leak the fixture.
                safety_cleanup.callback(actual_run, f"uid={uid} libaws infra-rm infra.yaml")
                safety_cleanup.callback(actual_run, f"libaws ecr-rm test-{uid}")
            if command == "libaws ecr-login":
                raise RuntimeError("injected login failure")
            return actual_run(command, *args, **kwargs)

        with patch.dict(test.__globals__, run=fail_login):
            with pytest.raises(RuntimeError, match="injected login failure"):
                test(tmp_path)
        assert uid is not None
        assert_cleanup(uid)
        safety_cleanup.pop_all()


@pytest.mark.parametrize("failure", ["infra-create", "repository-create", "login", "push", "image-cleanup"])
def test_cleanup_on_failure(tmp_path, failure):
    uid = "abcdef123456"
    commands = []
    inventory_reads = 0
    existing = {
        "account": "guarded", "region": "test-region",
        "infraset": {f"test-infraset-{uid}": {"lambda": {f"test-lambda-{uid}": {
            "attr": ["timeout=60"], "policy": ["AWSLambdaBasicExecutionRole"],
            "trigger": [{"type": "ecr"}],
        }}}},
    }

    def fake_run(command, *args, **kwargs):
        nonlocal inventory_reads
        commands.append(command)
        if command == "libaws aws-account":
            return "guarded"
        if command.startswith("libaws infra-ls"):
            inventory_reads += 1
            return yaml.safe_dump({"infraset": {}} if inventory_reads == 1 else existing)
        if ((failure == "infra-create" and command == "libaws infra-ensure infra.yaml")
                or (failure == "repository-create" and command.startswith("libaws ecr-ensure "))
                or (failure == "login" and command == "libaws ecr-login")
                or (failure == "push" and command.startswith("docker push "))
                or (failure == "image-cleanup" and command.startswith("docker image rm "))):
            raise RuntimeError("injected failure")
        if command.startswith("libaws logs-tail"):
            return uid
        return ""

    with patch.dict(os.environ, LIBAWS_TEST_ACCOUNT="guarded", DOCKER_HOST="unix:///not-used"), \
            patch.object(uuid, "uuid4", return_value=uid), patch.dict(test.__globals__, run=fake_run):
        with pytest.raises(RuntimeError, match="injected failure"):
            test(tmp_path)
    assert "libaws infra-rm infra.yaml" in commands, commands
    if failure != "infra-create":
        assert f"libaws ecr-rm test-{uid}" in commands, commands
    if failure in ("push", "image-cleanup"):
        assert any(c.startswith("docker image rm ") and f"libaws-ecr-test:{uid}" in c for c in commands), commands
        assert any(c.startswith("docker image rm ") and f"/test-{uid}:{uid}" in c for c in commands), commands


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
