#!/usr/bin/env python3
#
# conf: concurrency 0
# conf: memory 128
# conf: timeout 60
# policy: AWSLambdaBasicExecutionRole
# sns: test-sns
# trigger: sns test-sns

def main(event, context):
    """
    >>> import shell, uuid
    >>> run = lambda *a, **kw: shell.run(*a, stream=True, **kw)
    >>> path = __file__
    >>> uid = str(uuid.uuid4())[-12:]

    >>> _ = run(f'aws-lambda-rm -ey {path}')

    >>> _ = run(f'aws-lambda-deploy {path} -y && sleep 5 # iam is slow')

    >>> _ = run(f"aws-sns-publish test-sns {uid}")

    >>> assert uid == run(f'aws-lambda-logs {path} -f -e {uid} | tail -n1').split()[-1]

    >>> _ = run('aws-lambda-rm -ey', path)

    """
    msg = event['Records'][0]['Sns']['Message']
    print(msg)
