# type: ignore
import json
import os
import subprocess
import sys
import uuid

import pytest
import shell
import yaml

run = lambda *a, **kw: shell.run(*a, stream=True, **kw)


def assert_source_account_permission(function_name, service):
    policy = json.loads(run(f"libaws lambda-permissions {function_name}"))
    statements = [
        statement
        for statement in policy["Statement"]
        if statement.get("Principal", {}).get("Service") == service
    ]
    assert len(statements) == 1, statements
    assert (
        statements[0]["Condition"]["StringEquals"]["AWS:SourceAccount"]
        == os.environ["LIBAWS_TEST_ACCOUNT"]
    )
    assert statements[0]["Condition"]["ArnLike"]["AWS:SourceArn"]


def captured(*argv):
    result = subprocess.run(argv, text=True, capture_output=True)
    sys.stdout.write(result.stdout)
    sys.stderr.write(result.stderr)
    assert result.returncode == 0, result
    return result.stdout + result.stderr


def listed(uid):
    infra = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
    region = infra.pop("region")
    account = infra.pop("account")
    infra["infraset"][f"test-infraset-{uid}"].pop("keypair", None)
    return infra, region, account


def expected(uid, domain):
    return {
        "infraset": {
            f"test-infraset-{uid}": {
                "lambda": {
                    f"test-lambda-{uid}": {
                        "attr": ["timeout=60"],
                        "policy": ["AWSLambdaBasicExecutionRole"],
                        "trigger": [
                            {
                                "type": "ses",
                                "attr": [
                                    f"dns={domain}",
                                    f"bucket=test-ses-bucket-{uid}",
                                    "prefix=emails/",
                                ],
                            }
                        ],
                        "env": [f"uid={uid}"],
                    }
                },
                "s3": {
                    f"test-ses-bucket-{uid}": {
                        "attr": ["allow_put=ses.amazonaws.com", "acl=private"]
                    }
                },
            }
        }
    }


def receipt_rules():
    return run("libaws ses-ls-receipt-rules")


def test():
    assert os.environ["LIBAWS_TEST_ACCOUNT"] == run("libaws aws-account")
    os.environ["uid"] = uid = str(uuid.uuid4())[-12:]
    function_name = f"test-lambda-{uid}"
    bucket = f"test-ses-bucket-{uid}"
    domain = f"test-ses-{uid}.{os.environ['LIBAWS_TEST_DOMAIN']}"

    try:
        infra = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
        assert infra.get("infraset", {}) == {}, infra

        run("libaws infra-ensure infra.yaml --preview")
        run("libaws infra-ensure infra.yaml")

        infra, region, account = listed(uid)
        assert account == os.environ["LIBAWS_TEST_ACCOUNT"]
        assert infra == expected(uid, domain), infra
        assert domain in receipt_rules()
        assert_source_account_permission(function_name, "ses.amazonaws.com")

        function_arn = f"arn:aws:lambda:{region}:{account}:function:{function_name}"
        captured(
            "libaws",
            "ses-ensure-receipt-rule",
            domain,
            function_arn,
            bucket,
            "emails/",
        )

        assert f'"{uid}"' == run(
            f"libaws lambda-invoke {function_name} --payload-string '{{}}'"
        )

        run(
            "LIBAWS_INTEGRATION=1 "
            f"LIBAWS_LAMBDA_DELETE_TEST_FUNCTION={function_name} "
            "go test ../../../../lib -run '^TestLambdaManualDeleteIntegration$' -count=1 -v"
        )
        preview = captured("libaws", "infra-rm", "infra.yaml", "--preview")
        assert "deleted SES receipt rule:" in preview, preview
        assert domain in receipt_rules()

        run("libaws infra-rm infra.yaml")
        assert domain not in receipt_rules()
        infra = yaml.safe_load(run(f"libaws infra-ls --env-values --infraset test-infraset-{uid}"))
        assert infra.get("infraset", {}) == {}, infra
    finally:
        run("libaws infra-rm infra.yaml")
        if domain in receipt_rules():
            run(f"libaws ses-rm-receipt-rule {domain}")
        assert domain not in receipt_rules()


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
