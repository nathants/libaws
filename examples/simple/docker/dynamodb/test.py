# type: ignore
import pytest
import sys
import shell
import yaml
import json
import uuid
import os

run = lambda *a, **kw: shell.run(*a, stream=True, **kw)

def test():
    assert os.environ['CLI_AWS_TEST_ACCOUNT'] == run('libaws aws-account')
    account = os.environ['account'] = run('libaws aws-account')
    region = os.environ['region'] = run('libaws aws-region')
    os.environ['digest'] = 'fake'
    container = f'{account}.dkr.ecr.{region}.amazonaws.com/test-container'
    repo_name = container.split('amazonaws.com/')[-1]
    os.environ['uid'] = uid = str(uuid.uuid4())[-12:]
    infra = yaml.safe_load(run('libaws infra-ls'))
    assert sorted(infra["infraset"].keys()) == ["none"], infra
    assert sorted(infra["infraset"]["none"].keys()) == ["user"], infra
    run(f'docker build -t {container} --network host .')
    run(f'libaws ecr-ensure {repo_name}')
    run('libaws ecr-login')
    lines = run(f"docker push {container}").splitlines()
    digest = [x for x in lines[-1].split()
              if x.startswith('sha256:')][0]
    os.environ['digest'] = digest
    run('libaws infra-ensure infra.yaml --preview')
    run('libaws infra-ensure infra.yaml')
    infra = yaml.safe_load(run('libaws infra-ls'))
    infra.pop("region")
    infra.pop("account")
    infra["infraset"].pop("none")
    expected = {
        "infraset": {
            f"test-infraset-{uid}": {
                "dynamodb": {
                    f"test-other-table-{uid}": {"key": ["userid:s:hash"]},
                    f"test-table-{uid}": {"attr": ["stream=keys_only"],
                                          "key": ["userid:s:hash",
                                                  "version:n:range"]},
                },
                "lambda": {
                    f"test-lambda-{uid}": {
                        "allow": [f"dynamodb:GetItem arn:aws:dynamodb:*:*:table/test-table-{uid}",
                                  f"dynamodb:PutItem arn:aws:dynamodb:*:*:table/test-other-table-{uid}"],
                        "attr": ["timeout=60"],
                        "policy": ["AWSLambdaDynamoDBExecutionRole",
                                   "AWSLambdaBasicExecutionRole"],
                        "trigger": [{"attr": [f"test-table-{uid}",
                                              "batch=1",
                                              "parallel=10",
                                              "retry=0",
                                              "start=trim_horizon",
                                              "window=1"],
                                     "type": "dynamodb"}],
                        "env": [f"uid={uid}"],
                    }
                },
            }
        }
    }
    assert infra == expected, infra
    run(f'libaws dynamodb-item-put test-table-{uid} userid:s:jane version:n:1 data:s:{uid}')
    run(f'libaws logs-tail /aws/lambda/test-lambda-{uid} --from-hours 1 --exit-after "put:"')
    assert uid == json.loads(run(f'libaws dynamodb-item-get test-other-table-{uid} userid:s:jane'))['data']
    run('libaws infra-rm infra.yaml --preview')
    run(f'libaws ecr-rm {repo_name}')
    run('libaws infra-rm infra.yaml')
    infra = yaml.safe_load(run("libaws infra-ls"))
    assert sorted(infra["infraset"].keys()) == ["none"], infra
    assert sorted(infra["infraset"]["none"].keys()) == ["user"], infra

if __name__ == '__main__':
    sys.exit(pytest.main([__file__, "-svvx", "--tb", "native"]))
