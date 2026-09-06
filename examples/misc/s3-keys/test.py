from contextlib import ExitStack
from datetime import datetime
import os
from pathlib import Path
import subprocess
import time
import urllib.request
import uuid


DIRECTORY = Path(__file__).parent


def run(*args, input=None):
    print("$ libaws", *args, flush=True)
    return subprocess.check_output(["libaws", *args], input=input, text=True, cwd=DIRECTORY).strip()


def test():
    assert run("aws-account") == os.environ["LIBAWS_TEST_ACCOUNT"]
    os.environ["uid"] = uuid.uuid4().hex[:12]
    bucket = "test-s3-keys-" + os.environ["uid"]
    keys = ["a/./b/c", "a/../b/c", "a//b/c", "a/s3://b/c"]

    def verify_absent():
        # Name-based verification, independent of membership tags.
        assert bucket not in run("s3-ls").splitlines()

    with ExitStack() as cleanup:
        cleanup.callback(verify_absent)
        cleanup.callback(run, "s3-rm-bucket", bucket)
        cleanup.callback(run, "infra-rm", "infra.yaml")
        run("infra-ensure", "infra.yaml")
        for key in keys:
            uri = f"s3://{bucket}/{key}"
            run("s3-put", uri, input="original")
            assert run("s3-get", uri) == "original"
            first = run("s3-ls-versions", uri).split()
            assert len(first) == 6 and first[2] == "c" and first[-1] == "LATEST", first
            original_version = first[-2]
            assert original_version in run("s3-head", uri)
            assert run("s3-ls", uri, "--quiet") == f"{bucket}/{key}"
            assert run("s3-ls", uri, "--quiet", "--recursive") == f"{bucket}/{key}"
            assert run("s3-ls", uri).split()[3] == "c"
            assert run("s3-ls", uri, "--recursive").split()[3] == key
            # No client-side filesystem normalization of presigned paths either.
            with urllib.request.urlopen(run("s3-presign-get", uri), timeout=30) as response:
                assert response.read() == b"original"
            time.sleep(1.1)  # Distinct S3 version timestamps, not a sort tie.
            put = urllib.request.Request(run("s3-presign-put", uri), data=b"updated", method="PUT")
            with urllib.request.urlopen(put, timeout=30) as response:
                assert response.status == 200
            assert run("s3-get", uri) == "updated"
            assert run("s3-get-version", uri, "--version", original_version) == "original"
            time.sleep(1.1)
            run("s3-rm", uri)
            versions = [line.split() for line in run("s3-ls-versions", uri).splitlines()]
            assert [row[-1] for row in versions] == ["LATEST-DELETE", "HISTORICAL", "HISTORICAL"], versions
            assert all(len(row) == 6 and row[2] == "c" for row in versions), versions
            dates = [datetime.fromisoformat(row[0]) for row in versions]
            assert all(date.tzinfo is not None for date in dates), dates
            assert dates == sorted(dates, reverse=True), dates
            assert all(line.split()[2] == key for line in run("s3-ls-versions", uri, "--recursive").splitlines())
            run("s3-rm-versions", uri, "--version", original_version)
            assert original_version not in run("s3-ls-versions", uri)
            run("s3-rm-versions", uri)
            assert run("s3-ls-versions", uri) == ""

        # Prefixes and recursive/start-after quiet output remain full literal keys.
        for key in keys:
            run("s3-put", f"{bucket}/{key}", input="retained")
        assert run("s3-ls", f"{bucket}/a/./", "--quiet") == f"{bucket}/a/./b/"
        assert run("s3-ls", f"{bucket}/a/./").split() == ["PRE", "b/"]
        assert run("s3-ls-versions", f"{bucket}/a/./").split() == ["PRE", "b/"]
        assert run("s3-ls", f"{bucket}/a/", "--quiet", "--recursive").splitlines() == [f"{bucket}/{key}" for key in sorted(keys)]
        start = "a/./b/c"
        assert run("s3-ls", f"{bucket}/{start}", "--quiet", "--start-after").splitlines() == [f"{bucket}/{key}" for key in sorted(keys) if key > start]


if __name__ == "__main__":
    test()
