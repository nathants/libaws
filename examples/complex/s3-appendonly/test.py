# type: ignore
import os
import pathlib
import subprocess
import time
import uuid

DIRECTORY = pathlib.Path(__file__).parent


def captured(arguments, env=None):
    return subprocess.run(
        arguments,
        cwd=DIRECTORY,
        env=env,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )


def run(arguments):
    result = captured(arguments)
    if result.returncode != 0:
        raise AssertionError(result.stderr)
    return result.stdout.strip()


def create_key(username):
    result = captured(["libaws", "iam-ensure-user-api-key", username])
    assert result.returncode == 0, result.stderr
    values = {}
    for line in result.stdout.splitlines():
        key, value = line.split(": ", 1)
        values[key] = value
    assert sorted(values) == ["access key id", "access key secret"], values
    second = captured(["libaws", "iam-ensure-user-api-key", username])
    assert second.returncode == 0 and second.stdout == "", (second.stdout, second.stderr)
    return values["access key id"], values["access key secret"]


def credential_env(access_key, secret_key):
    env = os.environ.copy()
    for name in ["AWS_PROFILE", "AWS_SESSION_TOKEN", "AWS_SECURITY_TOKEN"]:
        env.pop(name, None)
    env["AWS_ACCESS_KEY_ID"] = access_key
    env["AWS_SECRET_ACCESS_KEY"] = secret_key
    return env


def client(binary, env, action, bucket, key, *extra):
    return captured([binary, action, bucket, key, *extra], env=env)


def test():
    assert os.environ["LIBAWS_TEST_ACCOUNT"] == run(["libaws", "aws-account"])
    os.environ["uid"] = uid = str(uuid.uuid4())[-12:]
    infra_name = f"libaws-appendonly-{uid}"
    bucket = infra_name
    writer_name = f"libaws-appendonly-writer-{uid}"
    reader_name = f"libaws-appendonly-reader-{uid}"
    missing_name = writer_name + "-missing"
    binary = f"/tmp/libaws-appendonly-client-{uid}"
    created = False
    try:
        missing = captured(["libaws", "iam-ensure-user-api-key", missing_name])
        assert missing.returncode != 0 and missing.stdout == "", (missing.stdout, missing.stderr)
        run(["go", "build", "-o", binary, "client.go"])
        run(["libaws", "infra-ensure", "infra.yaml", "--preview"])
        created = True  # infra-ensure can fail after creating a subset of the resources
        run(["libaws", "infra-ensure", "infra.yaml"])
        run(["libaws", "infra-ensure", "infra.yaml", "--preview"])

        listed = run(["libaws", "infra-ls", infra_name, "--env-values"])
        for value in [
            infra_name + ":",
            bucket + ":",
            writer_name + ":",
            reader_name + ":",
            "- acl=private",
            "- appendonly=true",
            "- versioning=true",
            f"- s3:PutObject arn:aws:s3:::{bucket}/*",
            f"- s3:GetObject arn:aws:s3:::{bucket}/*",
            f"- s3:GetObjectVersion arn:aws:s3:::{bucket}/*",
            f"- s3:ListBucket arn:aws:s3:::{bucket}",
        ]:
            assert value in listed, (value, listed)
        assert "acl=custom" not in listed, listed

        writer_env = credential_env(*create_key(writer_name))
        reader_env = credential_env(*create_key(reader_name))
        key = "retained-probe"

        for attempt in range(30):
            result = client(binary, writer_env, "create", bucket, key)
            if result.returncode == 0:
                break
            readable = client(binary, reader_env, "get", bucket, key)
            if readable.returncode == 0 and readable.stdout == "original":
                break
            if attempt == 29:
                raise AssertionError(result.stderr)
            time.sleep(2)

        for attempt in range(30):
            read = client(binary, reader_env, "get", bucket, key)
            if read.returncode == 0 and read.stdout == "original":
                break
            if attempt == 29:
                raise AssertionError(read.stderr)
            time.sleep(2)

        assert client(binary, writer_env, "create", bucket, key).returncode != 0
        assert client(binary, writer_env, "overwrite", bucket, key).returncode != 0
        assert client(binary, writer_env, "get", bucket, key).returncode != 0
        assert client(binary, writer_env, "list", bucket, key).returncode != 0

        read = client(binary, reader_env, "get", bucket, key)
        assert read.returncode == 0 and read.stdout == "original", (read.stdout, read.stderr)
        listing = client(binary, reader_env, "list", bucket, key)
        assert listing.returncode == 0 and listing.stdout.splitlines() == [key], (listing.stdout, listing.stderr)
        assert client(binary, reader_env, "create", bucket, "reader-write").returncode != 0

        version = client(binary, reader_env, "version", bucket, key)
        assert version.returncode == 0 and version.stdout, version.stderr
        assert client(binary, writer_env, "delete", bucket, key).returncode != 0
        assert client(binary, writer_env, "delete-version", bucket, key, version.stdout).returncode != 0
        assert client(binary, writer_env, "copy", bucket, key).returncode != 0
        read = client(binary, reader_env, "get", bucket, key)
        assert read.returncode == 0 and read.stdout == "original", (read.stdout, read.stderr)

        run(["libaws", "infra-rm", "infra.yaml", "--preview"])
    finally:
        pathlib.Path(binary).unlink(missing_ok=True)
        missing_cleanup = captured(["libaws", "iam-rm-user", missing_name])
        result = captured(["libaws", "infra-rm", "infra.yaml"]) if created else None
        assert missing_cleanup.returncode == 0, missing_cleanup.stderr
        if result is not None:
            assert result.returncode == 0, result.stderr

    listed = run(["libaws", "infra-ls", infra_name, "--env-values"])
    assert infra_name not in listed, listed


if __name__ == "__main__":
    test()
