# type: ignore
import os
import sys
import uuid

import pytest
import shell


def test():
    assert os.environ["LIBAWS_TEST_ACCOUNT"] == shell.run("libaws aws-account")
    os.environ["uid"] = str(uuid.uuid4())[-12:]
    os.environ["LIBAWS_DYNAMODB_RETENTION_INFRA"] = os.path.abspath("infra.yaml")
    os.environ["LIBAWS_DYNAMODB_RETENTION_UID"] = os.environ["uid"]
    shell.run("go test ../../../lib -run '^TestDynamoDBRetentionMonitoringIntegration$' -count=1 -timeout=25m -v", stream=True)


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
