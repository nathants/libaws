# type: ignore
from contextlib import ExitStack
from pathlib import Path
import subprocess
import uuid
import pytest
import sys
import shell
import yaml
import time
import os

run = lambda *a, **kw: shell.run(*a, stream=True, **kw)

def test(tmp_path):
    assert os.environ['LIBAWS_TEST_ACCOUNT'] == run('libaws aws-account')
    if not os.environ.get("DOCKER_HOST"):
        os.environ["DOCKER_HOST"] = run(
            "docker context inspect --format '{{.Endpoints.docker.Host}}'"
        )
    docker_config = tmp_path / "docker"
    docker_config.mkdir()
    os.environ["DOCKER_CONFIG"] = str(docker_config)
    os.environ['uid'] = uid = str(uuid.uuid4())[-12:]
    account = os.environ['account'] = run('libaws aws-account')
    region = os.environ['region'] = run('libaws aws-region')
    os.environ['digest'] = 'fake'
    container = f'{account}.dkr.ecr.{region}.amazonaws.com/test-container-{uid}'
    repo_name = container.split('amazonaws.com/')[-1]
    infra = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
    assert infra.get("infraset", {}) == {}, infra
    with ExitStack() as cleanup:
        # Register before provisioning; one cleanup failure must not block another.
        cleanup.callback(run, f"libaws ecr-rm {repo_name}")
        cleanup.callback(run, "libaws infra-rm infra.yaml")
        run(f'docker buildx build --provenance=false -t {container} --network host .')
        cleanup.callback(run, f"docker image rm {container}")
        run(f'libaws ecr-ensure {repo_name}')
        run('libaws ecr-login')
        lines = run(f"docker push {container}").splitlines()
        digest = [x for x in lines[-1].split()
                  if x.startswith('sha256:')][0]
        os.environ['digest'] = digest
        run('libaws infra-ensure infra.yaml --preview')
        run('libaws infra-ensure infra.yaml')
        infra = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
        infra.pop("region")
        infra.pop("account")
        infra["infraset"][f"test-infraset-{uid}"].pop("keypair", None)
        infra["infraset"][f"test-infraset-{uid}"]["lambda"][f"test-lambda-{uid}"]['trigger'][1].pop("attr")
        expected = {
            "infraset": {
                f"test-infraset-{uid}": {
                    "lambda": {
                        f"test-lambda-{uid}": {
                            "attr": ["timeout=60"],
                            "policy": ["AWSLambdaBasicExecutionRole"],
                            "trigger": [
                                {"attr": ["rate(15 minutes)"],
                                 "type": "schedule"},
                                {"type": "api"},
                            ],
                        }
                    }
                }
            }
        }
        assert infra == expected, infra
        url = run(f'libaws infra-url-api infra.yaml test-lambda-{uid}')
        print(url)
        for _ in range(10):
            try:
                run(f'curl -f {url} 2>/dev/null')
            except:
                time.sleep(1)
            else:
                break
        else:
            assert False, 'fail'
        assert 'ok' == run(f'curl {url} 2>/dev/null')
        # Publish changed code; quick mode must use the image, never a local ZIP.
        quick_context = tmp_path / "quick-image"
        quick_context.mkdir()
        (quick_context / "Dockerfile").write_text(Path("Dockerfile").read_text())
        source = Path("main.js").read_text()
        updated = source.replace("'ok\\n'", "'quick\\n'")
        assert updated != source
        (quick_context / "main.js").write_text(updated)
        run("docker buildx build --provenance=false -t", container, "--network none", str(quick_context))
        lines = run(f"docker push {container}").splitlines()
        os.environ["digest"] = [x for x in lines[-1].split() if x.startswith("sha256:")][0]
        run(f"libaws infra-ensure infra.yaml --quick test-lambda-{uid} --preview")
        assert 'ok' == run(f'curl -f {url} 2>/dev/null')
        run(f"libaws infra-ensure infra.yaml --quick test-lambda-{uid}")
        assert 'quick' == run(f'curl -f {url} 2>/dev/null')
        run('libaws infra-rm infra.yaml --preview')
    infra = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
    assert infra.get("infraset", {}) == {}, infra

    assert repo_name not in run("libaws ecr-ls").splitlines()
    for image in (container,):
        result = subprocess.run(["docker", "image", "inspect", image], capture_output=True, text=True)
        assert result.returncode != 0 and "No such image" in result.stderr, result
    result = subprocess.run(["libaws", "lambda-describe", f"test-lambda-{uid}"], capture_output=True, text=True)
    assert result.returncode != 0 and "ResourceNotFoundException" in result.stderr, result

if __name__ == '__main__':
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
