#!/usr/bin/env python3.8
#
# policy: CloudWatchLogsFullAccess
# include: include_me.txt

import os

def main(event, context):
    """
    >>> import shell, uuid
    >>> run = lambda *a, **kw: shell.run(*a, stream=True, **kw)
    >>> path = 'examples/lambda_includes.py'
    >>> uid = str(uuid.uuid4())

    >>> _ = run(f'aws-lambda-deploy {path} SOME_UUID={uid} -y')

    >>> assert f'"data123 {uid}"' == run(f'aws-lambda-invoke {path}')

    """
    with open('include_me.txt') as f:
        return f'{f.read().strip()} {os.environ["SOME_UUID"]}'
