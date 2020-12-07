#!/usr/bin/env python3
#
# conf: concurrency 0
# conf: memory 128
# conf: timeout 60
# policy: AWSLambdaBasicExecutionRole
# include: include_me.txt

import os

def main(event, context):
    """
    >>> import shell, uuid
    >>> run = lambda *a, **kw: shell.run(*a, stream=True, **kw)
    >>> path = __file__
    >>> uid = str(uuid.uuid4())[-12:]

    >>> _ = run(f'aws-lambda-deploy {path} UUID={uid} -y')

    >>> assert f'"data123 {uid}"' == run(f'aws-lambda-invoke {path}')

    >>> _ = run('aws-lambda-rm -ey', path)

    """
    with open('include_me.txt') as f:
        return f'{f.read().strip()} {os.environ["UUID"]}'
