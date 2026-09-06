from contextlib import ExitStack
from decimal import Decimal
import json
import os
from pathlib import Path
import subprocess
import time
import uuid

import yaml


DIRECTORY = Path(__file__).parent


def run(*args):
    print("$", *args, flush=True)
    return subprocess.check_output(args, cwd=DIRECTORY, text=True).strip()


def verify_item_numbers_and_scan(name):
    numbers = [
        "12345678901234567890123456789012345678",
        "0.12345678901234567890123456789012345678",
        "1e-130",
        "-9007199254740993",
        "9.9999999999999999999999999999999999999e125",
    ]
    expected = {}
    for index, number in enumerate(numbers):
        key = f"item-{index}"
        run("libaws", "dynamodb-item-put", name, f"id:s:{key}", "created:n:1", f"number:n:{number}")
        expected[key] = {"id": key, "created": 1, "number": Decimal(number)}

    for key, item in expected.items():
        # Wait only for eventual visibility, never retry a precision assertion.
        deadline = time.monotonic() + 30
        while True:
            result = subprocess.run(["libaws", "dynamodb-item-get", name, f"id:s:{key}", "created:n:1"], capture_output=True, text=True)
            if result.returncode == 0:
                break
            assert result.returncode == 1 and not result.stderr and time.monotonic() < deadline, result
            time.sleep(0.5)
        assert json.loads(result.stdout, parse_float=Decimal) == item, result.stdout

    for limit, page_size in ((0, 2), (3, 2), (1, 1), (10, 2)):
        output = run("libaws", "dynamodb-item-scan", name, "--limit", str(limit), "--page-size", str(page_size))
        items = [json.loads(line, parse_float=Decimal) for line in output.splitlines()]
        assert len(items) == (min(limit, len(expected)) if limit else len(expected)), output
        assert len({item["id"] for item in items}) == len(items), output
        for item in items:
            assert item == expected[item["id"]], item


def test():
    assert run("libaws", "aws-account") == os.environ["LIBAWS_TEST_ACCOUNT"]
    os.environ["uid"] = uuid.uuid4().hex[:12]
    name = "test-ddb-index-" + os.environ["uid"]

    def verify_absent():
        assert name not in run("libaws", "dynamodb-ls").splitlines()

    with ExitStack() as cleanup:
        cleanup.callback(verify_absent)
        cleanup.callback(run, "libaws", "dynamodb-rm", name)
        cleanup.callback(run, "libaws", "infra-rm", "infra.yaml")
        run("libaws", "infra-ensure", "infra.yaml", "--preview")
        run("libaws", "infra-ensure", "infra.yaml")
        print(run("env", f"LIBAWS_DDB_INDEX_TEST_TABLE={name}", "go", "test", "../../../lib", "-run", "^TestDynamoDBIndexesIntegration$", "-count=1", "-v"), flush=True)
        before = run("libaws", "infra-ls", "--infraset", name)
        table = yaml.safe_load(before)["infraset"][name]["dynamodb"][name]
        attrs = set(table["attr"])
        assert {
            "GlobalSecondaryIndexes.0.ProvisionedThroughput.ReadCapacityUnits=2",
            "GlobalSecondaryIndexes.0.ProvisionedThroughput.WriteCapacityUnits=3",
            "GlobalSecondaryIndexes.0.Projection.ProjectionType=INCLUDE",
            "LocalSecondaryIndexes.0.Projection.ProjectionType=KEYS_ONLY",
        } <= attrs, table
        run("libaws", "infra-ensure", "infra.yaml")
        assert run("libaws", "infra-ls", "--infraset", name) == before
        verify_item_numbers_and_scan(name)


if __name__ == "__main__":
    test()
