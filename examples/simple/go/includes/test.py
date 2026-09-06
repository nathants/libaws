# type: ignore
from pathlib import Path
from tempfile import TemporaryDirectory
import subprocess
import uuid
import pytest
import sys
import shell
import yaml
import os

run = lambda *a, **kw: shell.run(*a, stream=True, **kw)


def test():
    assert os.environ["LIBAWS_TEST_ACCOUNT"] == run("libaws aws-account")
    os.environ['uid'] = uid = str(uuid.uuid4())[-12:]
    infra = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
    assert infra.get("infraset", {}) == {}, infra
    try:
        run("libaws infra-ensure infra.yaml --preview")
        run("libaws infra-ensure infra.yaml")
        with TemporaryDirectory() as caller:
            declaration = str(Path(__file__).resolve().with_name("infra.yaml"))
            run("libaws infra-ensure", declaration, "--preview", cwd=caller)
            run("libaws infra-ensure", declaration, cwd=caller)
            run("libaws infra-ensure", declaration, "--quick", f"test-lambda-{uid}", cwd=caller)
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
                        }
                    }
                }
            }
        }
        assert infra == expected, infra
        assert '"data123"' == run(f"libaws lambda-invoke test-lambda-{uid}")
        run("libaws infra-rm infra.yaml --preview")
    finally:
        run("libaws infra-rm infra.yaml")
    result = subprocess.run(["libaws", "lambda-describe", f"test-lambda-{uid}"], capture_output=True, text=True)
    assert result.returncode != 0 and "ResourceNotFoundException" in result.stderr, result
    infra = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
    assert infra.get("infraset", {}) == {}, infra


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
