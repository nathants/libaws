import subprocess
import sys
from pathlib import Path

import pytest

LIBAWS = Path(__file__).resolve().parents[3] / "libaws"


def libaws(*args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        [str(LIBAWS), *args],
        check=False,
        capture_output=True,
        text=True,
    )


def test_logs_events_requires_start_millis_and_accepts_explicit_zero():
    missing = libaws("logs-events", "group", "marker")
    assert missing.returncode == 2, missing
    assert "START-MILLIS is required" in missing.stdout, missing

    explicit_zero = libaws(
        "logs-events",
        "--start-millis",
        "0",
        "--end-millis",
        "0",
        "group",
        "marker",
    )
    assert explicit_zero.returncode != 0, explicit_zero
    assert "end milliseconds must be greater than start milliseconds" in (
        explicit_zero.stdout + explicit_zero.stderr
    ), explicit_zero


@pytest.mark.parametrize(
    ("command", "provider"),
    [
        ("s3-describe", "provider: AWS S3 only"),
        ("s3-ensure", "provider: AWS S3 only"),
        ("s3-get", "providers: AWS S3 (default), Cloudflare R2 (--r2)"),
        ("s3-get-version", "provider: AWS S3 only"),
        ("s3-head", "providers: AWS S3 (default), Cloudflare R2 (--r2)"),
        ("s3-ls", "providers: AWS S3 (default), Cloudflare R2 (--r2)"),
        ("s3-ls-versions", "provider: AWS S3 only"),
        ("s3-presign-get", "provider: AWS S3 only"),
        ("s3-presign-put", "provider: AWS S3 only"),
        ("s3-put", "providers: AWS S3 (default), Cloudflare R2 (--r2)"),
        ("s3-rm", "providers: AWS S3 (default), Cloudflare R2 (--r2)"),
        ("s3-rm-bucket", "provider: AWS S3 only"),
        ("s3-rm-versions", "provider: AWS S3 only"),
    ],
)
def test_s3_command_provider_support_is_explicit(command: str, provider: str):
    result = libaws(command, "--help")
    assert result.returncode == 0, result
    assert provider in result.stdout, result.stdout


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
