import uuid
import pytest
import sys
import shell
import yaml
import time
import os

run = lambda *a, **kw: shell.run(*a, stream=True, **kw)

def test():
    assert os.environ["LIBAWS_TEST_ACCOUNT"] == run("libaws aws-account")

    os.environ["uid"] = uid = str(uuid.uuid4())[-12:]

    infra = yaml.safe_load(run("libaws infra-ls --env-values"))
    assert sorted(infra["infraset"].keys()) == ["none"], infra
    assert sorted(infra["infraset"]["none"].keys()) == ["user"], infra

    run("libaws infra-ensure infra.yaml --preview")
    run("libaws infra-ensure infra.yaml")

    infra = yaml.safe_load(run("libaws infra-ls --env-values"))
    infra.pop("region")
    infra.pop("account")
    infra["infraset"].pop("none")
    infra["infraset"][f"test-infraset-{uid}"].pop("keypair", None)
    infra["infraset"][f"test-infraset-{uid}"]["lambda"][f"test-lambda-{uid}"]['trigger'][0].pop("attr")
    infra["infraset"][f"test-infraset-{uid}"]["lambda"][f"test-lambda-{uid}"]['trigger'][1].pop("attr")
    expected = {
        "infraset": {
            f"test-infraset-{uid}": {
                "lambda": {
                    f"test-lambda-{uid}": {
                        "attr": ["timeout=60"],
                        "policy": ["AWSLambdaBasicExecutionRole"],
                        "trigger": [
                            {"type": "url"},
                            {"type": "api"},
                        ],
                    }
                }
            }
        }
    }
    assert infra == expected, infra

    api_url = run(f"libaws infra-url-api infra.yaml test-lambda-{uid}")
    fn_url = run(f"libaws infra-url-func infra.yaml test-lambda-{uid}")

    for _ in range(10):
        try:
            run(f"curl -f {api_url} 2>/dev/null")
            run(f"curl -f {fn_url} 2>/dev/null")
        except Exception:
            time.sleep(1)
        else:
            break
    else:
        assert False, "endpoints not live"

    assert "ok" == run(f"curl {api_url} 2>/dev/null")

    chunks = []
    prev_time = None

    def on_line(_kind, line):
        nonlocal prev_time
        now = time.time()
        if prev_time is not None:
            assert now - prev_time >= 1, f"chunk arrived too quickly: {now - prev_time}s"
        chunks.append(line)
        prev_time = now

    run(f"curl -s --no-buffer {fn_url}/stream", callback=on_line)
    assert chunks == ["a", "b", "c", "d"], f"unexpected stream chunks: {chunks}"

    run("libaws infra-rm infra.yaml --preview")
    run("libaws infra-rm infra.yaml")

    infra = yaml.safe_load(run("libaws infra-ls --env-values"))
    assert sorted(infra["infraset"].keys()) == ["none"], infra
    assert sorted(infra["infraset"]["none"].keys()) == ["user"], infra

if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
