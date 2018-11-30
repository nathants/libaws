#!/usr/bin/env python3.7
#
# policy: CloudWatchLogsFullAccess
# trigger: cloudwatch cron(* * * * ? *) # or: cloudwatch rate(1 minute)

import os

def main(event, context):
    """
    >>> import shell, uuid
    >>> run = lambda *a, **kw: shell.run(*a, stream=True, **kw)
    >>> path = 'examples/lambda_scheduled.py'
    >>> uid = str(uuid.uuid4())

    >>> _ = run(f'aws-lambda-deploy {path} SOME_UUID={uid} -y').split(':')[-1]

    >>> assert uid == run(f'aws-lambda-logs {path} -f -e {uid} | tail -n1').split()[-1]

    """
    print(os.environ['SOME_UUID'])
