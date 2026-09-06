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


def run(*args, cwd=DIRECTORY, env=None):
    print("$", *args, flush=True)
    try:
        return subprocess.check_output(args, cwd=cwd, env=env, text=True).strip()
    except subprocess.CalledProcessError as error:
        print(error.output, flush=True)
        raise


def test():
    assert run(LIBAWS, "aws-account") == os.environ["LIBAWS_TEST_ACCOUNT"]
    os.environ["uid"] = uid = uuid.uuid4().hex[:12]
    vpc = f"test-vpc-{uid}"
    key = f"test-keypair-{uid}"
    infraset = f"test-ec2-subnets-{uid}"
    with tempfile.TemporaryDirectory(prefix="libaws-ec2-key-") as directory:
        run(LIBAWS, "ssh-keygen-ed25519", cwd=directory)
        os.environ["pubkey"] = (Path(directory) / "id_ed25519.pub").read_text().strip()
        try:
            run(LIBAWS, "infra-ensure", "infra.yaml")
            vpc_id = run(LIBAWS, "vpc-id", vpc)
            sg_id = run(LIBAWS, "ec2-id-sg", vpc, f"test-sg-{uid}")
            zones = set(run(LIBAWS, "ec2-ls-instance-zones", "t3.small").split())
            subnets = [line.split() for line in run(LIBAWS, "vpc-ls-subnets", vpc).splitlines()]
            eligible = sorted(subnet for subnet, zone in subnets if zone in zones)
            assert len(eligible) >= 2, "this regression requires a multi-subnet VPC"

            for mode, selection, expected in (
                ("vpc", ["--vpc", vpc], eligible),
                # Explicit subnet IDs must bypass VPC discovery, even when both
                # flags are given. Use an SG ID so its lookup needs no VPC name.
                ("explicit", ["--vpc", f"nonexistent-{uid}", "--subnets", eligible[0]], eligible[:1]),
            ):
                instance_name = f"test-ec2-{mode}-{uid}"
                try:
                    instance_id = run(
                        "timeout", "--signal=INT", "--kill-after=30s", "300",
                        LIBAWS, "ec2-new", instance_name,
                        "--spot", "lowestPrice", "--type", "t3.small",
                        "--ami", "trixie", "--key", key, "--sg", sg_id,
                        "--gigs", "8", "--seconds-timeout", "600", *selection,
                    )
                    assert re.fullmatch(r"i-[0-9a-f]+", instance_id), instance_id
                    env = dict(
                        os.environ,
                        LIBAWS_EC2_SUBNET_TEST_INSTANCE=instance_id,
                        LIBAWS_EC2_SUBNET_TEST_VPC=vpc_id,
                        LIBAWS_EC2_SUBNET_TEST_SUBNETS=" ".join(expected),
                    )
                    # Inspect AWS's stored fleet request, not just CLI log output.
                    print(run(
                        "go", "test", "./lib", "-run", "^TestEC2SubnetsFromVpcIntegration$",
                        "-count=1", "-v", cwd=ROOT, env=env,
                    ), flush=True)
                finally:
                    run(LIBAWS, "ec2-rm", instance_name, "--wait")
        finally:
            run(LIBAWS, "infra-rm", "infra.yaml")

    inventory = yaml.safe_load(run(LIBAWS, "infra-ls", "--infraset", infraset))
    assert inventory.get("infraset", {}) == {}, inventory
    # Direct absence checks: a lost membership tag is not proof of deletion.
    assert vpc_id not in run(LIBAWS, "vpc-ls")
    assert key not in run(LIBAWS, "ec2-ls-keypairs")


if __name__ == "__main__":
    test()
