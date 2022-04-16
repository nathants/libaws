#!/usr/bin/env python3
#
# attr: concurrency 0
# attr: memory 128
# attr: timeout 60
# require: git+https://github.com/nathants/py-util
# require: requests >2, <3
# policy: AWSLambdaBasicExecutionRole

import requests
import util.colors
import os

def main(event, context):
    """
    >>> import shell, uuid
    >>> run = lambda *a, **kw: shell.run(*a, stream=True, **kw)
    >>> path = __file__
    >>> uid = str(uuid.uuid4())[-12:]

    >>> _ = run(f'cli-aws lambda-rm -ey {path}')

    >>> _ = run(f'cli-aws lambda-ensure {path} UUID={uid} -y && sleep 5 # iam is slow')

    >>> _ = run('cat - > /tmp/input', stdin='{"foo": "bar"}')

    >>> run(f'cli-aws lambda-invoke {path} -p /tmp/input')
    '"foo=>bar"'

    >>> assert uid == run(f'cli-aws lambda-logs {path} -f -e {uid} | tail -n1').split()[-1]

    >>> _ = run('cli-aws lambda-rm -ey', path)

    """
    print('log some stuff about requests:', requests.get)
    print(util.colors.green('green means go'))
    print(os.environ['UUID'])
    return ' '.join(f'{k}=>{v}' for k, v in event.items())
