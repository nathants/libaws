#!/usr/bin/env python3
#
# attr: concurrency 0
# attr: memory 128
# attr: timeout 60
# policy: AWSLambdaBasicExecutionRole
# include: include_me.txt

import os

def main(event, context):
    """
    >>> import shell, uuid
    >>> run = lambda *a, **kw: shell.run(*a, stream=True, **kw)
    >>> path = __file__
    >>> uid = str(uuid.uuid4())[-12:]

    >>> _ = run(f'cli-aws lambda-rm -ey {path}')

    >>> _ = run(f'cli-aws lambda-ensure {path} UUID={uid} -y && sleep 5 # iam is slow')

    >>> assert f'"data123 {uid}"' == run(f'cli-aws lambda-invoke {path}')

    >>> _ = run('cli-aws lambda-rm -ey', path)

    """
    with open('include_me.txt') as f:
        return f'{f.read().strip()} {os.environ["UUID"]}'
