from contextlib import ExitStack
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
                    # Give interrupted EC2 library calls their bounded five-minute
                    # cleanup window. This adds no delay to successful launches.
                    "timeout", "--signal=INT", "--kill-after=330s", "300",
                    LIBAWS, "ec2-new", instance_name, *market, "--type", "t3.small",
                    "--ami", "trixie", "--key", key, "--sg", sg_id,
                    "--gigs", "8", "--seconds-timeout", "600", *selection,
                )
                assert re.fullmatch(r"i-[0-9a-f]+", instance_id), instance_id
                env = dict(
                    os.environ,
                    LIBAWS_EC2_SUBNET_TEST_INSTANCE=instance_id,
                    LIBAWS_EC2_SUBNET_TEST_VPC=vpc_id,
                    LIBAWS_EC2_SUBNET_TEST_SUBNETS=" ".join(expected),
                    LIBAWS_EC2_SUBNET_TEST_LIFECYCLE=lifecycle,
                )
                run(
                    "go", "test", "./lib", "-run", "^TestEC2SubnetsFromVpcIntegration$",
                    "-count=1", "-v", cwd=ROOT, env=env, stream=True,
                )
                run(LIBAWS, "ec2-rm", instance_id, "--wait")
            run(
                "go", "test", "./lib", "-run", "^TestEC2SpotFleetFailureIntegration$",
                "-count=1", "-v", cwd=ROOT, env=dict(os.environ, LIBAWS_EC2_EXAMPLE_UID=uid), stream=True,
            )

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
