# type: ignore
from contextlib import ExitStack
import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile
import uuid

import pytest
import shell
import yaml

run = lambda *a, **kw: shell.run(*a, stream=True, **kw)
marker = "lambda-build-fixture"


def read_json(path):
    with open(path) as f:
        return json.load(f)


def sha256_file(path):
    digest = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest()


def reject(command, message):
    result = subprocess.run(command, capture_output=True, text=True, check=False)
    output = result.stdout + result.stderr
    assert result.returncode != 0 and message in output, result


def assert_function_absent(name):
    result = subprocess.run(
        ["libaws", "lambda-describe", name], capture_output=True, text=True, check=False
    )
    assert result.returncode != 0 and "ResourceNotFoundException" in result.stderr, result


def describe(name):
    # The code location is a presigned URL, so compare only stable fields.
    text = run(f"libaws lambda-describe {name}")
    decoder = json.JSONDecoder()
    function, end = decoder.raw_decode(text)
    configuration, _ = decoder.raw_decode(text[end:].lstrip())
    return function, configuration


def test():
    assert os.environ["LIBAWS_TEST_ACCOUNT"] == run("libaws aws-account")
    os.environ["uid"] = uid = str(uuid.uuid4())[-12:]
    name = f"test-lambda-{uid}"
    work = tempfile.mkdtemp(prefix="libaws-lambda-build-")
    build = os.path.join(work, "build")
    deploy = os.path.join(work, "deploy")
    with ExitStack() as cleanup:
        cleanup.callback(shutil.rmtree, work, ignore_errors=True)
        cleanup.callback(assert_function_absent, name)
        cleanup.callback(run, f"libaws lambda-rm {name}")
        cleanup.callback(run, "libaws infra-rm infra.yaml")
        infra = yaml.safe_load(run(f"libaws infra-ls --infraset test-infraset-{uid}"))
        assert infra.get("infraset", {}) == {}, infra
        assert_function_absent(name)
        # Empty values, invalid directories, and unwritable locations fail before
        # any build or AWS access.
        reject(["libaws", "infra-ensure", "infra.yaml", "--build-only", ""], "build directory must not be empty")
        reject(["libaws", "infra-ensure", "infra.yaml", "--from-build", ""], "build directory must not be empty")
        reject(["libaws", "infra-ensure", "infra.yaml", "--from-build", "infra.yaml"], "not a directory")
        os.makedirs(deploy)
        shutil.copy("infra.yaml", deploy)
        reject(["libaws", "infra-ensure", "infra.yaml", "--from-build", deploy], "read build manifest")
        if os.geteuid() != 0:
            readonly = os.path.join(work, "readonly")
            os.makedirs(readonly)
            os.chmod(readonly, 0o500)
            reject(["libaws", "infra-ensure", "infra.yaml", "--build-only", readonly], "not writable")
        run(f"libaws infra-ensure infra.yaml --build-only {build}")
        manifest = read_json(os.path.join(build, "manifest.json"))
        assert manifest["version"] == 1, manifest
        assert [artifact["name"] for artifact in manifest["artifacts"]] == [name], manifest
        artifact = manifest["artifacts"][0]
        assert artifact["file"] == f"{name}/bootstrap", artifact
        assert len(artifact["git_commit"]) == 40, artifact
        assert sha256_file(os.path.join(build, artifact["file"])) == artifact["sha256"], artifact
        # A tampered artifact fails hash verification before any provider call.
        tampered = os.path.join(work, "tampered")
        shutil.copytree(build, tampered)
        with open(os.path.join(tampered, artifact["file"]), "ab") as f:
            f.write(b"tampered")
        reject(["libaws", "infra-ensure", "infra.yaml", "--from-build", tampered], "sha256 mismatch")
        assert yaml.safe_load(run(f"libaws infra-ls --infraset test-infraset-{uid}")).get("infraset", {}) == {}
        assert_function_absent(name)
        assert f"/lambda/{name}-path/" not in run("libaws iam-ls-roles")
        assert f"/aws/lambda/{name}" not in run("libaws logs-ls")
        # Deploy the built bytes from a directory that has no source tree.
        run(f"libaws infra-ensure infra.yaml --from-build {build}", cwd=deploy)
        assert marker in run(f"libaws lambda-invoke {name}")
        _, configuration = describe(name)
        run(f"libaws infra-ensure infra.yaml --from-build {build}", cwd=deploy)
        _, redeployed = describe(name)
        assert (configuration["CodeSha256"], configuration["RevisionId"]) == (
            redeployed["CodeSha256"],
            redeployed["RevisionId"],
        ), (configuration, redeployed)
        assert marker in run(f"libaws lambda-invoke {name}")
    infra = yaml.safe_load(run(f"libaws infra-ls --infraset test-infraset-{uid}"))
    assert infra.get("infraset", {}) == {}, infra


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
