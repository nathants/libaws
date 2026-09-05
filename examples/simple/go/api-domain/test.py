# type: ignore
import math
import os
import shlex
import sys
import time
import urllib.error
import urllib.request
import uuid

import pytest
import shell
import yaml

run = lambda *args, **kwargs: shell.run(*args, stream=True, **kwargs)

WORK_SECONDS = 1200
CLEANUP_SECONDS = 300
OUTER_TIMEOUT_SECONDS = 1800


def run_before(deadline, args, env=None, warn=False):
    remaining = math.ceil(deadline - time.monotonic())
    if remaining <= 0:
        raise TimeoutError(f"deadline expired before: {shlex.join(args)}")
    command = (
        f"timeout --signal=TERM --kill-after=10s {remaining}s "
        f"{shlex.join(args)}"
    )
    return run(command, env=env, warn=warn)


def integration(uid, mode, deadline, warn=False):
    env = os.environ.copy()
    env.update(
        {
            "LIBAWS_INTEGRATION": "1",
            "LIBAWS_API_DOMAIN_TEST_UID": uid,
            "LIBAWS_API_DOMAIN_TEST_MODE": mode,
        }
    )
    return run_before(
        deadline,
        [
            "go",
            "test",
            "../../../../lib",
            "-run",
            "^TestLambdaAPIDomainIntegration$",
            "-count=1",
            "-v",
        ],
        env=env,
        warn=warn,
    )


def delete_lambda(function, deadline):
    env = os.environ.copy()
    env.update(
        {
            "LIBAWS_INTEGRATION": "1",
            "LIBAWS_LAMBDA_DELETE_TEST_FUNCTION": function,
        }
    )
    run_before(
        deadline,
        [
            "go",
            "test",
            "../../../../lib",
            "-run",
            "^TestLambdaManualDeleteIntegration$",
            "-count=1",
            "-v",
        ],
        env=env,
    )


def assert_https(domain, deadline):
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    error = None
    while time.monotonic() < deadline:
        try:
            timeout = max(0.1, min(10, deadline - time.monotonic()))
            with opener.open(f"https://{domain}/", timeout=timeout) as response:
                body = response.read().decode()
                assert response.status == 200, response.status
                assert body == "ok", body
                return
        except (AssertionError, OSError, urllib.error.URLError) as current_error:
            error = current_error
            time.sleep(min(3, max(0, deadline - time.monotonic())))
    raise AssertionError(f"HTTPS endpoint {domain!r} did not become ready: {error}")


def assert_listing(uid, zone, deadline, http_dns=True):
    listed = yaml.safe_load(
        run_before(deadline, ["libaws", "infra-ls", "--env-values", "--infraset", f"test-api-domain-set-{uid}"])
    )
    listed.pop("region")
    listed.pop("account")
    name = f"test-api-domain-set-{uid}"
    listed["infraset"][name].pop("keypair", None)
    for function in [
        f"test-lambda-api-domain-http-{uid}",
        f"test-lambda-api-domain-websocket-{uid}",
    ]:
        trigger = listed["infraset"][name]["lambda"][function]["trigger"][0]
        trigger["attr"] = [
            attr for attr in trigger["attr"] if not attr.startswith("url=")
        ]
    expected = {
        "infraset": {
            name: {
                "lambda": {
                    f"test-lambda-api-domain-http-{uid}": {
                        "attr": ["timeout=60"],
                        "policy": ["AWSLambdaBasicExecutionRole"],
                        "trigger": [
                            {
                                "type": "api",
                                "attr": [
                                    f"{'dns' if http_dns else 'domain'}=api-{uid}.{zone}"
                                ],
                            }
                        ],
                    },
                    f"test-lambda-api-domain-websocket-{uid}": {
                        "attr": ["timeout=60"],
                        "policy": ["AWSLambdaBasicExecutionRole"],
                        "trigger": [
                            {
                                "type": "websocket",
                                "attr": [f"domain=ws-{uid}.{zone}"],
                            }
                        ],
                    },
                }
            }
        }
    }
    assert listed == expected, listed


def test():
    started = time.monotonic()
    work_deadline = started + WORK_SECONDS
    cleanup_deadline = started + WORK_SECONDS + CLEANUP_SECONDS
    assert WORK_SECONDS + CLEANUP_SECONDS < OUTER_TIMEOUT_SECONDS

    assert os.environ["LIBAWS_TEST_ACCOUNT"] == run_before(
        work_deadline, ["libaws", "aws-account"]
    )
    zone = os.environ["LIBAWS_TEST_DOMAIN"]
    os.environ["uid"] = uid = uuid.uuid4().hex[-12:]
    http_function = f"test-lambda-api-domain-http-{uid}"
    websocket_function = f"test-lambda-api-domain-websocket-{uid}"
    http_domain = f"api-{uid}.{zone}"
    try:
        integration(uid, "ensure-fixture", work_deadline)
        integration(uid, "verify-removed", work_deadline)
        initial = yaml.safe_load(
            run_before(work_deadline, ["libaws", "infra-ls", "--env-values", "--infraset", f"test-api-domain-set-{uid}"])
        )
        assert initial.get("infraset", {}) == {}, initial

        run_before(work_deadline, ["libaws", "infra-ensure", "infra.yaml", "--preview"])
        integration(uid, "verify-removed", work_deadline)
        run_before(work_deadline, ["libaws", "infra-ensure", "infra.yaml"])
        integration(uid, "verify-created", work_deadline)
        assert_listing(uid, zone, work_deadline)

        assert_https(
            http_domain, min(work_deadline, time.monotonic() + 240)
        )
        assert run_before(
            work_deadline, ["libaws", "infra-ensure", "infra.yaml", "--preview"]
        ) == ""

        run_before(
            work_deadline,
            [
                "libaws",
                "route53-ensure-record",
                zone,
                http_domain,
                "Type=A",
                "TTL=60",
                "Value=192.0.2.1",
            ],
        )
        integration(uid, "verify-simple-record", work_deadline)
        assert_listing(uid, zone, work_deadline, http_dns=False)
        run_before(work_deadline, ["libaws", "infra-ensure", "infra.yaml", "--preview"])
        integration(uid, "verify-simple-record", work_deadline)
        run_before(work_deadline, ["libaws", "infra-ensure", "infra.yaml"])
        integration(uid, "verify-created", work_deadline)
        assert_listing(uid, zone, work_deadline)
        assert_https(
            http_domain, min(work_deadline, time.monotonic() + 240)
        )

        delete_lambda(http_function, work_deadline)
        delete_lambda(websocket_function, work_deadline)
        run_before(work_deadline, ["libaws", "infra-rm", "infra.yaml", "--preview"])
        integration(uid, "verify-created", work_deadline)
        run_before(work_deadline, ["libaws", "infra-rm", "infra.yaml"])
        integration(uid, "verify-removed", work_deadline)
        integration(uid, "verify-fixture", work_deadline)
    finally:
        result = integration(uid, "cleanup", cleanup_deadline, warn=True)
        assert result["exitcode"] == 0, result


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
