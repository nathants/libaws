# type: ignore
import time
import pytest
import shlex
import sys
import uuid
import shell
import yaml
import os

run = lambda *a, **kw: shell.run(*a, stream=True, **kw)


def test():
    assert os.environ["LIBAWS_TEST_ACCOUNT"] == run("libaws aws-account")
    os.environ["uid"] = uid = str(uuid.uuid4())[-12:]
    run(f"mkdir -p /tmp/{uid}")
    run(f"cd /tmp/{uid} && libaws ssh-keygen-ed25519")
    os.environ["pubkey"] = run(f"cat /tmp/{uid}/id_ed25519.pub")
    infra = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
    assert infra.get("infraset", {}) == {}, infra
    run("libaws infra-ensure infra.yaml --preview")
    run("libaws infra-ensure infra.yaml")
    infra = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
    infra.pop("region")
    infra.pop("account")
    infra["infraset"][f"test-infraset-{uid}"].pop("keypair", None)
    expected = {
        "infraset": {
            f"test-infraset-{uid}": {
                "instance-profile": {
                    f"test-profile-{uid}": {
                        "allow": [
                            f"s3:GetObject arn:aws:s3:::in-bucket-{uid}/*",
                            f"s3:List* arn:aws:s3:::in-bucket-{uid}/*",
                            "s3:PutObject "
                            f"arn:aws:s3:::out-bucket-{uid}/*",
                        ]
                    }
                },
                "lambda": {
                    f"test-lambda-{uid}": {
                        "allow": [
                            "ec2:* *",
                            "iam:GetRole *",
                            "iam:PassRole arn:aws:iam::*:role/aws-ec2-spot-fleet-tagging-role",
                            f"iam:PassRole arn:aws:iam::*:role/ec2/test-profile-{uid}-path/test-profile-{uid}",
                        ],
                        "policy": ["AWSLambdaBasicExecutionRole"],
                        "trigger": [
                            {
                                "attr": [f"in-bucket-{uid}"],
                                "type": "s3",
                            }
                        ],
                        'env': [f'uid={uid}'],
                    }
                },
                "s3": {
                    f"in-bucket-{uid}": {'attr': ['acl=private']},
                    f"out-bucket-{uid}": {'attr': ['acl=private']},
                },
                "vpc": {
                    f"test-vpc-{uid}": {
                        "security-group": {
                            f"test-sg-{uid}": {
                                "rule": ["tcp:22:0.0.0.0/0"],
                            }
                        }
                    }
                },
            }
        }
    }
    assert infra == expected, infra
    key = "test key;$(false).txt"
    source = shlex.quote(f"s3://in-bucket-{uid}/{key}")
    destination = shlex.quote(f"s3://out-bucket-{uid}/{key}")
    run(f"echo hello | libaws s3-put {source}")
    for i in range(100):
        try:
            assert "hello from ec2" == run(f"libaws s3-get {destination}")
        except:
            if i > 12:
                raise
            time.sleep(10)
        else:
            break
    for i in range(100):
        infra = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
        infra.pop("region")
        infra.pop("account")
        infra["infraset"][f"test-infraset-{uid}"].pop("keypair", None)
        try:
            assert infra == expected
        except:
            if i > 12:
                raise
            print('wait for ec2 to shutdown') # infra-rm is not allowed if any running ec2 instances exist
            time.sleep(10)
        else:
            break
    run("libaws infra-rm infra.yaml --preview")
    run(f"rm -rf /tmp/{uid}")
    run("libaws infra-rm infra.yaml")
    infra = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
    assert infra.get("infraset", {}) == {}, infra


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
