# type: ignore
import pytest
import sys
import uuid
import shell
import yaml
import time
import os

run = lambda *a, **kw: shell.run(*a, stream=True, **kw)


def test():
    assert os.environ["LIBAWS_TEST_ACCOUNT"] == run("libaws aws-account")
    os.environ['uid'] = uid = str(uuid.uuid4())[-12:]
    infra = yaml.safe_load(run("libaws infra-ls --env-values"))
    assert sorted(infra["infraset"].keys()) == ["none"], infra
    assert sorted(infra["infraset"]["none"].keys()) == ["user"], infra
    run("libaws infra-ensure infra.yaml --preview")
    run("libaws infra-ensure infra.yaml")
    infra = yaml.safe_load(run("libaws infra-ls --env-values"))
    infra.pop("region")
    infra.pop("account")
    infra["infraset"].pop("none")
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
    url = run(f"libaws infra-url-api infra.yaml test-lambda-{uid}")
    for _ in range(10):
        try:
            run(f"curl -f {url} 2>/dev/null")
        except:
            time.sleep(1)
        else:
            break
    else:
        assert False, "fail"
    assert '["GET", "/", null]' == run(f"curl {url} 2>/dev/null")
    assert '["POST", "/a/b/c/d/e/f/g", "%s"]' % uid == run(f"curl {url}/a/b/c/d/e/f/g -d {uid} 2>/dev/null")
    run("libaws infra-rm infra.yaml --preview")
    run("libaws infra-rm infra.yaml")
    infra = yaml.safe_load(run("libaws infra-ls --env-values"))
    assert sorted(infra["infraset"].keys()) == ["none"], infra
    assert sorted(infra["infraset"]["none"].keys()) == ["user"], infra


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
