#!/usr/bin/env python3.7
#
# require: ./dependencies
# policy: CloudWatchLogsFullAccess

import foo
import os

def main(event, context):
    """
    >>> import shell, uuid
    >>> run = lambda *a, **kw: shell.run(*a, stream=True, **kw)
    >>> path = 'examples/lambda_dependencies.py'
    >>> uid = str(uuid.uuid4())

    >>> _ = run(f'aws-lambda-deploy {path} SOME_UUID={uid} -y')

    >>> _ = run(f'aws-lambda-invoke {path}')

    >>> assert uid == run(f'aws-lambda-logs {path} -f -e {uid} | tail -n1').split()[-1]

    """
    print(foo.bar(os.environ['SOME_UUID']))
