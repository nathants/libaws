# type: ignore
import time
import pytest
import sys
import uuid
import shell
import yaml
import os

run = lambda *a, **kw: shell.run(*a, stream=True, **kw)


def test():
    assert os.environ["CLI_AWS_TEST_ACCOUNT"] == run("libaws aws-account")
    os.environ["uid"] = uid = str(uuid.uuid4())[-12:]
    run(f"mkdir -p /tmp/{uid}")
    run(f"cd /tmp/{uid} && libaws ssh-keygen-ed25519")
    os.environ["pubkey"] = run(f"cat /tmp/{uid}/id_ed25519.pub")
    infra = yaml.safe_load(run("libaws infra-ls"))
    assert sorted(infra["infraset"].keys()) == ["none"], infra
    assert sorted(infra["infraset"]["none"].keys()) == ["user"], infra
    run("libaws infra-ensure infra.yaml --preview")
    run("libaws infra-ensure infra.yaml")
    infra = yaml.safe_load(run("libaws infra-ls"))
    infra.pop("region")
    infra.pop("account")
    infra["infraset"].pop("none")
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
                    }
                },
                "s3": {
                    f"in-bucket-{uid}": {},
                    f"out-bucket-{uid}": {},
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
    run(f"echo hello | aws s3 cp - s3://in-bucket-{uid}/test-key.txt")
    for i in range(100):
        try:
            assert "hello from ec2" == run(f"aws s3 cp s3://out-bucket-{uid}/test-key.txt -")
        except:
            if i > 12:
                raise
            time.sleep(10)
        else:
            break
    for i in range(100):
        infra = yaml.safe_load(run("libaws infra-ls"))
        infra.pop("region")
        infra.pop("account")
        infra["infraset"].pop("none")
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
    infra = yaml.safe_load(run("libaws infra-ls"))
    assert sorted(infra["infraset"].keys()) == ["none"], infra
    assert sorted(infra["infraset"]["none"].keys()) == ["user"], infra


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
