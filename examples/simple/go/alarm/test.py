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


def captured(*argv):
    result = subprocess.run(argv, text=True, capture_output=True)
    sys.stdout.write(result.stdout)
    sys.stderr.write(result.stderr)
    assert result.returncode == 0, result
    return result.stdout + result.stderr


def listed(uid):
    infra = yaml.safe_load(run(f"libaws infra-ls --env-values {uid}"))
    region = infra.pop("region")
    account = infra.pop("account")
    infra["infraset"].pop("none")
    infra["infraset"][f"test-infraset-{uid}"].pop("keypair", None)
    return infra, region, account


def expected(uid):
    return {
        "infraset": {
            f"test-infraset-{uid}": {
                "lambda": {
                    f"test-lambda-{uid}": {
                        "attr": ["timeout=60"],
                        "policy": ["AWSLambdaBasicExecutionRole"],
                        "trigger": [
                            {
                                "type": "alarm",
                                "attr": [
                                    f"name=test-alarm-invocations-{uid}",
                                    f"lambda-invocations=test-lambda-{uid}",
                                    "at-least=1/minute",
                                ],
                            },
                        ],
                        "env": [f"uid={uid}"],
                    }
                }
            }
        }
    }


def cloudwatch_alarm_names():
    output = run("libaws cloudwatch-ls-alarms")
    return {
        json.loads(line)["AlarmName"]
        for line in output.splitlines()
        if line.strip()
    }


def run_alarm_integration(mode, function_name, alarm_name):
    run(
        "LIBAWS_INTEGRATION=1 "
        f"LIBAWS_LAMBDA_ALARM_TEST_FUNCTION={function_name} "
        f"LIBAWS_LAMBDA_ALARM_TEST_ALARM={alarm_name} "
        f"LIBAWS_LAMBDA_ALARM_TEST_MODE={mode} "
        "go test ../../../../lib -run '^TestLambdaAlarmIntegration$' -count=1 -v"
    )


def test():
    assert os.environ["LIBAWS_TEST_ACCOUNT"] == run("libaws aws-account")
    os.environ["uid"] = uid = str(uuid.uuid4())[-12:]
    function_name = f"test-lambda-{uid}"
    invocation_name = f"test-alarm-invocations-{uid}"
    alarm_names = {invocation_name}

    try:
        infra = yaml.safe_load(run(f"libaws infra-ls --env-values {uid}"))
        assert infra["infraset"] == {"none": None}, infra
        run("libaws infra-ensure infra.yaml")

        infra, region, account = listed(uid)
        assert account == os.environ["LIBAWS_TEST_ACCOUNT"]
        assert infra == expected(uid), infra
        assert alarm_names <= cloudwatch_alarm_names()

        permissions = json.loads(run(f"libaws lambda-permissions {function_name}"))
        alarm_permissions = [
            statement
            for statement in permissions["Statement"]
            if statement.get("Principal", {}).get("Service")
            == "lambda.alarms.cloudwatch.amazonaws.com"
        ]
        expected_function_arn = (
            f"arn:aws:lambda:{region}:{account}:function:{function_name}"
        )
        expected_alarm_arns = {
            f"arn:aws:cloudwatch:{region}:{account}:alarm:{name}"
            for name in alarm_names
        }
        assert len(alarm_permissions) == 1, alarm_permissions
        assert {statement["Resource"] for statement in alarm_permissions} == {
            expected_function_arn
        }
        assert {
            statement["Condition"]["ArnLike"]["AWS:SourceArn"]
            for statement in alarm_permissions
        } == expected_alarm_arns
        assert {
            statement["Condition"]["StringEquals"]["AWS:SourceAccount"]
            for statement in alarm_permissions
        } == {account}

        assert f'"{uid}"' == run(
            f"libaws lambda-invoke {function_name} --payload-string '{{}}'"
        )
        alarm_event = run(
            f"timeout 300 libaws logs-tail /aws/lambda/{function_name} "
            f"--from-hours 1 --exit-after {invocation_name}"
        )
        assert invocation_name in alarm_event

        run_alarm_integration("drift", function_name, invocation_name)
        drifted, _, _ = listed(uid)
        expected_without_alarm = expected(uid)
        del expected_without_alarm["infraset"][f"test-infraset-{uid}"][
            "lambda"
        ][function_name]["trigger"]
        assert drifted == expected_without_alarm, drifted

        preview = captured("libaws", "infra-ensure", "infra.yaml", "--preview")
        assert preview.count("updated CloudWatch metric alarm:") == 1, preview
        run_alarm_integration("verify-drift", function_name, invocation_name)

        run("libaws infra-ensure infra.yaml")
        run_alarm_integration("verify-converged", function_name, invocation_name)

        converged = captured("libaws", "infra-ensure", "infra.yaml", "--preview")
        assert "CloudWatch metric alarm" not in converged, converged

        run_alarm_integration("delete-function", function_name, invocation_name)
        removal = captured("libaws", "infra-rm", "infra.yaml", "--preview")
        assert removal.count("deleted CloudWatch metric alarm:") == 1, removal
    finally:
        run("libaws infra-rm infra.yaml")
        infra = yaml.safe_load(run(f"libaws infra-ls --env-values {uid}"))
        assert infra["infraset"] == {"none": None}, infra
        remaining_alarms = alarm_names & cloudwatch_alarm_names()
        if remaining_alarms:
            run_alarm_integration("force-cleanup", function_name, invocation_name)
            remaining_alarms = alarm_names & cloudwatch_alarm_names()
        assert not remaining_alarms, remaining_alarms
        log_groups = {line.split()[0] for line in run("libaws logs-ls").splitlines()}
        assert f"/aws/lambda/{function_name}" not in log_groups


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
