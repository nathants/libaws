# type: ignore
from contextlib import ExitStack
from pathlib import Path
import os
import pytest
import shell
import shutil
import sys
import uuid
import yaml

run = lambda *a, **kw: shell.run(*a, stream=True, **kw)


def test(tmp_path):
    assert os.environ["LIBAWS_TEST_ACCOUNT"] == run("libaws aws-account")
    os.environ["uid"] = uid = str(uuid.uuid4())[-12:]
    source = tmp_path / "source"
    shutil.copytree(Path(__file__).parent, source, ignore=shutil.ignore_patterns(".venv", "__pycache__", "*.egg-info", "build", "dist"))
    definition = str(source / "infra.yaml")
    # Neither this file nor the caller's cwd may override the declaration's
    # requirements.txt, which also contains a relative local-package path.
    (tmp_path / "requirements.txt").write_text("--invalid-caller-requirement\n")

    def infra(command, *options, **kwargs):
        return run("libaws", command, definition, *options, cwd=tmp_path, raw_cmd=True, **kwargs)

    inventory = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
    assert inventory.get("infraset", {}) == {}, inventory
    with ExitStack() as cleanup:
        cleanup.callback(infra, "infra-rm")
        infra("infra-ensure", "--preview")
        infra("infra-ensure")
        infra("infra-ensure", "--quick", f"test-lambda-{uid}")
        converged = infra("infra-ensure", "--preview", warn=True)
        assert converged["exitcode"] == 0 and "preview: zip " not in converged["stderr"], converged
        entrypoint = source / "main.py"
        original = entrypoint.read_text()
        entrypoint.write_text(original + "\n# deliberate ZIP drift control\n")
        changed = infra("infra-ensure", "--preview", warn=True)
        assert changed["exitcode"] == 0 and "preview: zip " in changed["stderr"], changed
        entrypoint.write_text(original)
        restored = infra("infra-ensure", "--preview", warn=True)
        assert restored["exitcode"] == 0 and "preview: zip " not in restored["stderr"], restored
        inventory = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
        inventory.pop("region")
        inventory.pop("account")
        inventory["infraset"][f"test-infraset-{uid}"].pop("keypair", None)
        expected = {
            "infraset": {
                f"test-infraset-{uid}": {
                    "lambda": {
                        f"test-lambda-{uid}": {
                            "attr": ["timeout=60"],
                            "policy": ["AWSLambdaBasicExecutionRole"],
                            "env": [f"uid={uid}"],
                        }
                    }
                }
            }
        }
        assert inventory == expected, inventory
        run(f"libaws lambda-invoke test-lambda-{uid}")
        assert uid == run(f"libaws logs-tail /aws/lambda/test-lambda-{uid} --from-hours 1 --exit-after {uid} | tail -n1").split()[-1]
        infra("infra-rm", "--preview")
    inventory = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
    assert inventory.get("infraset", {}) == {}, inventory
    result = run(f"libaws lambda-describe test-lambda-{uid}", warn=True)
    assert result["exitcode"] != 0 and "ResourceNotFoundException" in result["stderr"], result


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
