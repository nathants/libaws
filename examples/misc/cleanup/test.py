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
FALLBACKS = ["simple/go/alarm", "simple/go/ses", "misc/basic"]
LIVE_EXAMPLES = ["simple/go/ecr", "simple/docker/ecr", "complex/s3-ec2", "misc/ec2", *PLAIN, *FALLBACKS]


class InjectedFailure(RuntimeError):
    pass


def load(example):
    spec = importlib.util.spec_from_file_location("cleanup_example", ROOT / "examples" / example / "test.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def invoke(module, example, tmp_path):
    if example == "complex/s3-ec2" or example in PLAIN or example in FALLBACKS:
        module.test()
    else:
        module.test(tmp_path)


CASES = [(example, failure) for example in EXAMPLES for failure in (
    ("infra-create", "cleanup-errors") if example == "complex/s3-ec2" else
    ("infra-create", "repository-create", "build", "login", "push", "cleanup-errors")
)] + [(example, "image-cleanup") for example in ("simple/go/ecr", "simple/docker/ecr")] + [
    (example, "event-timeout") for example in ("simple/go/ecr", "simple/docker/ecr", "simple/python/ecr")
]


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
        if "libaws logs-tail " in command:
            if failure == "event-timeout":
                assert command.startswith("timeout --kill-after=5s 180 libaws logs-tail "), command
                raise InjectedFailure("bounded ECR event wait timed out")
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


@pytest.mark.parametrize("example,failure", [
    ("simple/go/alarm", "primary"), ("simple/go/alarm", "fallback"),
    ("simple/go/ses", "primary"), ("simple/go/ses", "fallback"),
    ("misc/basic", "primary"), ("misc/basic", "local"),
])
@pytest.mark.parametrize("stage", ["create", "post-create inventory"])
def test_independent_finalizers(example, failure, stage, tmp_path):
    module = load(example)
    uid = "abcdef123456"
    commands = []
    created = cleaning = fallback_failed = False
    alarms = set()
    domain = f"test-ses-{uid}.example.invalid"

    def fake_run(command, *args, **kwargs):
        nonlocal created, cleaning, fallback_failed
        commands.append(command)
        if command == "libaws aws-account":
            return "guarded"
        if command == "mktemp":
            return str(tmp_path / "input")
        if command == "libaws infra-ensure infra.yaml":
            created = True
            alarms.update({f"test-alarm-invocations-{uid}", f"test-alarm-invocations_{uid}", os.environ.get("long_alarm_name", "")})
            if stage == "create":
                raise InjectedFailure("partial create")
        if command == "libaws infra-rm infra.yaml":
            cleaning = True
            if failure == "primary":
                raise InjectedFailure("primary cleanup")
        if command == "rm -f":
            cleaning = True
            if failure == "local":
                raise InjectedFailure("local cleanup")
        if command.startswith("libaws infra-ls "):
            if created and not cleaning:
                raise InjectedFailure("post-create inventory")
            return "account: guarded\nregion: test-region\n"
        if command == "libaws cloudwatch-ls-alarms":
            return "\n".join(json.dumps({"AlarmName": name}) for name in alarms)
        if command == "libaws ses-ls-receipt-rules":
            return domain if f"libaws ses-rm-receipt-rule {domain}" not in commands else ""
        if "MODE=force-cleanup" in command or command.startswith("libaws ses-rm-receipt-rule "):
            if failure == "fallback" and not fallback_failed:
                fallback_failed = True
                raise InjectedFailure("fallback cleanup")
            for value in shlex.split(command):
                if value.startswith("LIBAWS_LAMBDA_ALARM_TEST_ALARM="):
                    alarms.discard(value.split("=", 1)[1])
        return ""

    def fake_process(args, **kwargs):
        assert args[1] in ("lambda-describe", "infra-ensure"), args
        return subprocess.CompletedProcess(args, 1, "", "ResourceNotFoundException; environment size 4097 bytes; request size 5121 bytes")

    with patch.dict(os.environ, LIBAWS_TEST_ACCOUNT="guarded", LIBAWS_TEST_DOMAIN="example.invalid"), \
            patch.object(module.uuid, "uuid4", return_value=uid), \
            patch.object(module, "run", side_effect=fake_run), \
            patch.object(module.subprocess, "run", side_effect=fake_process):
        with pytest.raises((InjectedFailure, AssertionError)) as raised:
            module.test()
        error = raised.value
        while not isinstance(error, InjectedFailure) and error.__context__ is not None:
            error = error.__context__
        assert isinstance(error, InjectedFailure), raised.value
        assert created, commands
        assert "libaws infra-rm infra.yaml" in commands, commands
        if example == "simple/go/alarm":
            for name in (f"test-alarm-invocations-{uid}", f"test-alarm-invocations_{uid}", os.environ["long_alarm_name"]):
                assert any("MODE=force-cleanup" in command and f"LIBAWS_LAMBDA_ALARM_TEST_ALARM={name} " in command for command in commands), commands
        elif example == "simple/go/ses":
            assert f"libaws ses-rm-receipt-rule {domain}" in commands, commands
            assert f"libaws s3-rm-bucket test-ses-bucket-{uid}" in commands, commands
        else:
            assert "rm -f" in commands, commands
        assert f"libaws lambda-rm test-lambda-{uid}" in commands, commands


@pytest.mark.parametrize("example", LIVE_EXAMPLES)
def test_live_failure_cleanup(example, tmp_path):
    if example == "misc/ec2":
        ec2_live_failure_cleanup(tmp_path)
        return
    module = load(example)
    actual_run = module.run
    uid = None
    vpc_id = None
    created = False
    repositories = set()
    images = set()
    with patch.dict(os.environ), chdir(ROOT / "examples" / example), ExitStack() as safety:
        def fail_after_create(command, *args, **kwargs):
            nonlocal uid, vpc_id, created
            uid = os.environ.get("uid")
            if command.startswith("libaws ecr-ensure "):
                repository = command.split()[-1]
                repositories.add(repository)
                safety.callback(actual_run, f"libaws ecr-rm {repository}")
            if command == "libaws infra-ensure infra.yaml":
                safety.callback(actual_run, "libaws infra-rm infra.yaml")
                if example == "complex/s3-ec2":
                    safety.callback(actual_run, f"LIBAWS_S3_EC2_CLEANUP_UID={uid} LIBAWS_S3_EC2_DRAIN=yes go test ../../../lib -run '^TestS3EC2ExampleCleanup$' -count=1 -v")
            if created and example in FALLBACKS and command == "libaws infra-rm infra.yaml":
                raise InjectedFailure("after live create: primary cleanup")
            output = actual_run(command, *args, **kwargs)
            if command == "libaws infra-ensure infra.yaml":
                created = True
            if created and example == "misc/basic" and command == "rm -f":
                raise InjectedFailure("after live create: local cleanup")
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
        assert uid is not None and created
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


def ec2_live_failure_cleanup(tmp_path):
    module = load("misc/ec2")
    actual_run = module.run
    uid = None
    vpc_id = None
    fleet_file = tmp_path / "empty-fleet-id"
    with patch.dict(os.environ), ExitStack() as safety:
        def fail_with_empty_fleet(*args, **kwargs):
            nonlocal uid, vpc_id
            uid = os.environ.get("uid")
            if args[:2] == (module.LIBAWS, "infra-ensure"):
                safety.callback(actual_run, module.LIBAWS, "ec2-rm-keypair", f"test-keypair-{uid}")
                safety.callback(actual_run, module.LIBAWS, "vpc-rm", f"vpc-test-{uid}")
                safety.callback(module.cleanup_compute, uid)
            if args[0] == "timeout":
                # The real AWS request exists, but the simulated failed CLI has
                # not returned an instance ID. It cannot launch before cleanup.
                print(actual_run(
                    "go", "test", "./lib", "-run", "^TestEC2ExampleEmptyFleet$", "-count=1", "-v",
                    cwd=ROOT, env=dict(os.environ, LIBAWS_EC2_EXAMPLE_UID=uid, LIBAWS_EC2_EXAMPLE_FLEET_FILE=str(fleet_file)),
                ), flush=True)
                raise InjectedFailure("CLI failed with an owned empty fleet")
            result = actual_run(*args, **kwargs)
            if args[:2] == (module.LIBAWS, "vpc-id"):
                vpc_id = result
            return result

        with patch.object(module, "run", side_effect=fail_with_empty_fleet):
            with pytest.raises(InjectedFailure, match="owned empty fleet"):
                module.test()
        assert uid and vpc_id and fleet_file.is_file()
        print(actual_run(
            "go", "test", "./lib", "-run", "^TestEC2ExampleComputeAbsent$", "-count=1", "-v",
            cwd=ROOT, env=dict(os.environ, LIBAWS_EC2_EXAMPLE_UID=uid, LIBAWS_EC2_EXAMPLE_FLEET_FILE=str(fleet_file)),
        ), flush=True)
        inventory = yaml.safe_load(actual_run(module.LIBAWS, "infra-ls", "--infraset", f"test-ec2-subnets-{uid}"))
        assert inventory.get("infraset", {}) == {}, inventory
        assert vpc_id not in actual_run(module.LIBAWS, "vpc-ls")
        assert f"test-keypair-{uid}" not in actual_run(module.LIBAWS, "ec2-ls-keypairs")
        safety.pop_all()


@pytest.mark.parametrize("failure", ("provision", "launch", "compute-cleanup", "vpc-cleanup", "keypair-cleanup"))
def test_ec2_failure_cleanup(failure):
    module = load("misc/ec2")
    commands = []

    def fake_run(*args, cwd=module.DIRECTORY, env=None, stream=False):
        commands.append(args)
        if args[0] == "timeout":
            raise InjectedFailure("launch")
        if args[0] == "go":
            assert "^TestEC2ExampleCleanup$" in args, args
            if failure == "compute-cleanup":
                raise InjectedFailure("compute cleanup")
            return ""
        command = args[1]
        if command == "aws-account":
            return "guarded"
        if command == "ssh-keygen-ed25519":
            (Path(cwd) / "id_ed25519.pub").write_text("ssh-ed25519 test\n")
        if command == "infra-ensure" and failure == "provision":
            raise InjectedFailure("partial provision")
        if command == "vpc-rm" and failure == "vpc-cleanup":
            raise InjectedFailure("VPC cleanup")
        if command == "ec2-rm-keypair" and failure == "keypair-cleanup":
            raise InjectedFailure("keypair cleanup")
        return {
            "vpc-id": "vpc-0123456789abcdef0", "ec2-id-sg": "sg-0123456789abcdef0",
            "ec2-ls-instance-zones": "us-east-1a us-east-1b",
            "vpc-ls-subnets": "subnet-a us-east-1a\nsubnet-b us-east-1b",
        }.get(command, "")

    with patch.dict(os.environ, LIBAWS_TEST_ACCOUNT="guarded"), patch.object(module, "run", side_effect=fake_run):
        with pytest.raises(InjectedFailure):
            module.test()
        uid = os.environ["uid"]
    finalizers = [args for args in commands if (args[0] == "go" or args[1] in ("vpc-rm", "ec2-rm-keypair"))]
    assert len(finalizers) == 3, commands
    assert "^TestEC2ExampleCleanup$" in finalizers[0]
    assert finalizers[1] == (module.LIBAWS, "vpc-rm", f"vpc-test-{uid}")
    assert finalizers[2] == (module.LIBAWS, "ec2-rm-keypair", f"test-keypair-{uid}")


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
