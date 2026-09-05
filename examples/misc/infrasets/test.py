import concurrent.futures
import os
import pathlib
import subprocess
import urllib.request
import uuid

import yaml

DIRECTORY = pathlib.Path(__file__).parent


def run(*args):
    print("$", *args, flush=True)
    return subprocess.check_output(args, cwd=DIRECTORY, text=True).strip()


def listed(name):
    return yaml.safe_load(run("libaws", "infra-ls", "--infraset", name, "--env-values")).get("infraset", {})


def test():
    assert run("libaws", "aws-account") == os.environ["LIBAWS_TEST_ACCOUNT"]
    os.environ["uid"] = uid = uuid.uuid4().hex[:12]
    first = f"test-owned-{uid}"
    second = first + "-other"  # Exact membership, not a prefix match.
    function = f"test-worker-{uid}"
    bucket = f"test-object-store-{uid}"
    assert listed(first) == {}
    assert listed(second) == {}
    try:
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
            creations = [pool.submit(run, "libaws", "infra-ensure", path) for path in ("infra.yaml", "other.yaml")]
            for creation in creations:
                creation.result()
        # Re-ensure immediately after enabling TTL; both ensure and inventory
        # must reread a transitioning status rather than wait on a stale response.
        run("libaws", "infra-ensure", "other.yaml")
        a, b = listed(first), listed(second)
        assert set(a) == {first}, a
        assert set(b) == {second}, b
        assert set(a[first]) == {"lambda", "s3"}, a
        assert set(a[first]["lambda"]) == {function}, a
        assert set(a[first]["s3"]) == {bucket}, a
        assert set(b[second]) == {"dynamodb", "sqs", "user"}, b
        assert set(b[second]["dynamodb"]) == {f"test-records-{uid}"}, b
        assert "ttl=expires" in b[second]["dynamodb"][f"test-records-{uid}"]["attr"], b
        assert set(b[second]["sqs"]) == {f"test-queue-{uid}"}, b
        assert set(b[second]["user"]) == {f"test-reader-{uid}"}, b
        triggers = a[first]["lambda"][function]["trigger"]
        assert {x["type"] for x in triggers} == {"api", "s3"}, triggers
        assert next(x for x in triggers if x["type"] == "s3")["attr"] == [bucket], triggers
        url = next(attr.removeprefix("url=") for x in triggers if x["type"] == "api" for attr in x["attr"] if attr.startswith("url="))
        with urllib.request.urlopen(url, timeout=30) as response:
            assert response.read() == b"owned-set"

        # A remains intact and usable throughout B's asynchronous deletion.
        with concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
            removal = pool.submit(run, "libaws", "infra-rm", "other.yaml")
            checks = 0
            while not removal.done() or checks == 0:
                assert listed(first) == a
                with urllib.request.urlopen(url, timeout=30) as response:
                    assert response.read() == b"owned-set"
                checks += 1
            removal.result()
        print(f"verified {checks} scoped inventories while the other set was being removed")
        assert listed(second) == {}
        assert listed(first) == a
    finally:
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
            removals = [pool.submit(run, "libaws", "infra-rm", path) for path in ("infra.yaml", "other.yaml")]
            for removal in removals:
                removal.result()
    assert listed(first) == {}
    assert listed(second) == {}
    assert function not in run("libaws", "lambda-ls")
    assert bucket not in run("libaws", "s3-ls")
    assert f"test-records-{uid}" not in run("libaws", "dynamodb-ls")
    assert f"test-queue-{uid}" not in run("libaws", "sqs-ls")


if __name__ == "__main__":
    test()
