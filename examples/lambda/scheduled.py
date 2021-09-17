#!/usr/bin/env python3
#
# attr: concurrency 0
# attr: memory 128
# attr: timeout 60
# policy: AWSLambdaBasicExecutionRole
# trigger: cloudwatch rate(1 minute)

import os

def main(event, context):
    """
    >>> import shell, uuid
    >>> run = lambda *a, **kw: shell.run(*a, stream=True, **kw)
    >>> path = __file__
    >>> uid = str(uuid.uuid4())[-12:]

    >>> _ = run(f'aws-lambda-rm -ey {path}')

    >>> _ = run(f'aws-lambda-deploy {path} UUID={uid} -y && sleep 5 # iam is slow')

    >>> assert uid == run(f'aws-lambda-logs {path} -f -e {uid} | tail -n1').split()[-1]

    >>> _ = run('aws-lambda-rm -ey', path)

    """
    print(os.environ['UUID'])
