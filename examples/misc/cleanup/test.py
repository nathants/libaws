"""Exercise the actual example workflows, including their failure teardown."""
from contextlib import ExitStack, chdir
import importlib.util
import json
import os
from pathlib import Path
import shlex
import subprocess
import sys
import time
from unittest.mock import patch

import pytest
import yaml

ROOT = Path(__file__).resolve().parents[3]
DOCKER = [f"simple/docker/{name}" for name in ("api", "dynamodb", "ecr", "s3", "schedule", "sqs", "websocket")]
EXAMPLES = [*DOCKER, "simple/go/ecr", "complex/s3-ec2"]
PLAIN = [
    "misc/api_binary", "misc/api_deep_routes",
    *(f"simple/go/{name}" for name in ("api", "api_and_stream", "dynamodb", "includes", "s3", "schedule", "sqs", "websocket")),
    *(f"simple/python/{name}" for name in ("api", "dynamodb", "includes", "s3", "schedule", "sqs", "websocket")),
]
LIVE_EXAMPLES = ["simple/go/ecr", "simple/docker/ecr", "complex/s3-ec2", *PLAIN]


class InjectedFailure(RuntimeError):
    pass


def load(example):
    spec = importlib.util.spec_from_file_location("cleanup_example", ROOT / "examples" / example / "test.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def invoke(module, example, tmp_path):
    if example == "complex/s3-ec2" or example in PLAIN:
        module.test()
    else:
        module.test(tmp_path)


CASES = [(example, failure) for example in EXAMPLES for failure in (
    ("infra-create", "cleanup-errors") if example == "complex/s3-ec2" else
    ("infra-create", "repository-create", "build", "login", "push", "cleanup-errors")
)] + [(example, "image-cleanup") for example in ("simple/go/ecr", "simple/docker/ecr")]


@pytest.mark.parametrize("example,failure", CASES)
def test_failure_cleanup(example, failure, tmp_path):
    module = load(example)
    uid = "abcdef123456"
    commands = []
    inventory_reads = 0

    def fake_run(command, *args, **kwargs):
        nonlocal inventory_reads
        commands.append(command)
        if command == "libaws aws-account":
            return "guarded"
        if command == "libaws aws-region":
            return "test-region"
        if command.startswith("cat "):
            return "ssh-ed25519 test"
        if command.startswith("libaws infra-ls "):
            inventory_reads += 1
            return yaml.safe_dump({
                "account": "guarded", "region": "test-region",
                "infraset": {} if inventory_reads == 1 else {f"test-infraset-{uid}": {"lambda": {f"test-lambda-{uid}": {
                    "attr": ["timeout=60"], "policy": ["AWSLambdaBasicExecutionRole"], "trigger": [{"type": "ecr"}],
                }}}},
            })
        if command.startswith("libaws lambda-permissions "):
            return json.dumps({"Statement": [{
                "Principal": {"Service": "events.amazonaws.com"},
                "Condition": {"StringEquals": {"AWS:SourceAccount": "guarded"}, "ArnLike": {"AWS:SourceArn": "owned-rule"}},
            }]})
        if ((failure == "infra-create" and command == "libaws infra-ensure infra.yaml")
                or (failure == "repository-create" and command.startswith("libaws ecr-ensure "))
                or (failure == "build" and command.startswith("docker build"))
                or (failure in ("login", "cleanup-errors") and command == "libaws ecr-login")
                or (failure == "push" and command.startswith("docker push "))
                or (failure == "image-cleanup" and command.startswith("docker image rm "))
                or (failure == "cleanup-errors" and command.startswith(("docker image rm ", "libaws ecr-rm ", "LIBAWS_S3_EC2_CLEANUP_UID=")))
                or (example == "complex/s3-ec2" and command == "libaws infra-ensure infra.yaml")):
            raise InjectedFailure(command)
        if command.startswith("docker push "):
            return "latest: digest: sha256:" + "a" * 64 + " size: 42"
        if command.startswith("libaws logs-tail "):
            return uid
        return ""

    with patch.dict(os.environ, LIBAWS_TEST_ACCOUNT="guarded", DOCKER_HOST="unix:///not-used"), \
            patch.object(module.uuid, "uuid4", return_value=uid), patch.object(module, "run", side_effect=fake_run):
        with pytest.raises(InjectedFailure):
            invoke(module, example, tmp_path)
    assert "libaws infra-rm infra.yaml" in commands, commands
    for command in commands:
        if command.startswith("libaws ecr-ensure "):
            assert command.replace("ecr-ensure", "ecr-rm", 1) in commands, commands
        if command.startswith("docker tag "):
            image = command.removeprefix("docker tag ").split(" ", 1)[1]
            assert f"docker image rm {image}" in commands, commands
    if example in DOCKER and failure != "build":
        assert f"docker image rm guarded.dkr.ecr.test-region.amazonaws.com/test-container-{uid}" in commands, commands
    if example == "simple/go/ecr" and failure in ("push", "image-cleanup"):
        assert f"docker image rm libaws-ecr-test:{uid}" in commands, commands
        assert any(c.startswith("docker image rm ") and f"/test-ecr-{uid}:{uid}" in c for c in commands), commands
    if example == "complex/s3-ec2":
        assert any(c.startswith(f"LIBAWS_S3_EC2_CLEANUP_UID={uid} ") for c in commands), commands


@pytest.mark.parametrize("example", PLAIN)
@pytest.mark.parametrize("failure", ["create", "post-create inventory", "cleanup-error"])
def test_plain_failure_cleanup(example, failure, tmp_path):
    module = load(example)
    commands = []
    local_files = []
    inventory_reads = 0

    def fake_run(command, *args, **kwargs):
        nonlocal inventory_reads
        commands.append(command)
        if command == "libaws aws-account":
            return "guarded"
        if command.startswith("libaws infra-ls "):
            inventory_reads += 1
            if failure == "post-create inventory" and inventory_reads == 2:
                raise InjectedFailure(command)
            return "account: guarded\nregion: test-region\n"
        if ((command == "libaws infra-ensure infra.yaml" and failure in ("create", "cleanup-error"))
                or (command == "libaws infra-rm infra.yaml" and failure == "cleanup-error")):
            raise InjectedFailure(command)
        if command == "head -c 64 /dev/urandom >":
            local_file = Path(args[0])
            local_file.write_bytes(b"fixture")
            local_files.append(local_file)
        return ""

    with patch.dict(os.environ, LIBAWS_TEST_ACCOUNT="guarded"), patch.object(module, "run", side_effect=fake_run):
        with pytest.raises(InjectedFailure):
            invoke(module, example, tmp_path)
    assert "libaws infra-rm infra.yaml" in commands, commands
    if example == "misc/api_binary":
        assert local_files and all(not path.parent.exists() for path in local_files), local_files


@pytest.mark.parametrize("example", LIVE_EXAMPLES)
def test_live_failure_cleanup(example, tmp_path):
    module = load(example)
    actual_run = module.run
    uid = None
    vpc_id = None
    repositories = set()
    images = set()
    with patch.dict(os.environ), chdir(ROOT / "examples" / example), ExitStack() as safety:
        def fail_after_create(command, *args, **kwargs):
            nonlocal uid, vpc_id
            uid = os.environ.get("uid")
            if command.startswith("libaws ecr-ensure "):
                repository = command.split()[-1]
                repositories.add(repository)
                safety.callback(actual_run, f"libaws ecr-rm {repository}")
            if command == "libaws infra-ensure infra.yaml":
                safety.callback(actual_run, "libaws infra-rm infra.yaml")
                if example == "complex/s3-ec2":
                    safety.callback(actual_run, f"LIBAWS_S3_EC2_CLEANUP_UID={uid} LIBAWS_S3_EC2_DRAIN=yes go test ../../../lib -run '^TestS3EC2ExampleCleanup$' -count=1 -v")
            output = actual_run(command, *args, **kwargs)
            if command == f"libaws vpc-id test-vpc-{uid}":
                vpc_id = output
            if command.startswith("docker build"):
                image = shlex.split(command)[shlex.split(command).index("-t") + 1]
                images.add(image)
                safety.callback(actual_run, f"docker image rm {image}")
            if ((example != "complex/s3-ec2" and command == "libaws infra-ensure infra.yaml")
                    or (example == "complex/s3-ec2" and "| libaws s3-put " in command)):
                raise InjectedFailure("after live create")
            return output

        with patch.object(module, "run", side_effect=fail_after_create):
            with pytest.raises(InjectedFailure, match="after live create"):
                invoke(module, example, tmp_path)
        assert uid is not None
        inventory = yaml.safe_load(actual_run(f"libaws infra-ls --infraset test-infraset-{uid}"))
        assert inventory.get("infraset", {}) == {}, inventory
        result = subprocess.run(["libaws", "lambda-describe", f"test-lambda-{uid}"], capture_output=True, text=True)
        assert result.returncode != 0 and "ResourceNotFoundException" in result.stderr, result
        declaration = yaml.safe_load(os.path.expandvars((ROOT / "examples" / example / "infra.yaml").read_text()))
        for section, command in (("s3", "s3-ls"), ("dynamodb", "dynamodb-ls"), ("sqs", "sqs-ls")):
            if declaration.get(section):
                remaining = actual_run(f"libaws {command}")
                # SQS deletion is asynchronous (up to 60 seconds). Still verify
                # real queue names independently of their disappearing tags.
                if section == "sqs":
                    deadline = time.monotonic() + 90
                    while any(name in remaining for name in declaration[section]) and time.monotonic() < deadline:
                        time.sleep(1)
                        remaining = actual_run(f"libaws {command}")
                assert all(name not in remaining for name in declaration[section]), remaining
        if example in PLAIN:
            remaining = actual_run("libaws api-ls")
            assert all(name not in remaining for name in declaration.get("lambda", {})), remaining
        for repository in repositories:
            assert repository not in actual_run("libaws ecr-ls").splitlines()
        for image in images:
            result = subprocess.run(["docker", "image", "inspect", image], capture_output=True, text=True)
            assert result.returncode != 0 and "No such image" in result.stderr, result
        if example == "complex/s3-ec2":
            assert f"test-keypair-{uid}" not in actual_run("libaws ec2-ls-keypairs")
            assert vpc_id and vpc_id not in actual_run("libaws vpc-ls")
        safety.pop_all()


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
