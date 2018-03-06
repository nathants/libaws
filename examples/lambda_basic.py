#!/usr/bin/env python3.6
#
# require: git+https://github.com/nathants/py-util@e4aafbbb0f6e1bea793791356636968bef1924a2
# require: requests==2.18.4
# policy: CloudWatchLogsFullAccess
# allow: s3:List* *

import requests
import util.colors
import boto3
import os

def main(event, context):
    """
    >>> import shell
    >>> run = lambda *a, **kw: shell.run(*a, stream=True, **kw)
    >>> path = 'examples/lambda_basic.py'

    >>> run(f'aws-lambda-deploy {path} SOME_VAR=some_val -y').split(':')[-1]
    'lambda-basic'

    >>> run('cat - > /tmp/input', stdin='{"foo": "bar"}')
    ''
    >>> run(f'aws-lambda-invoke {path} -p /tmp/input')
    '{"foo": "bar"}'

    >>> run(f'aws-lambda-logs {path} -f -e some_val | tail -n1').split()[-1]
    'some_val'

    """
    print('log some stuff about requests:', requests.get)
    print('you have some buckets:', len(list(boto3.client('s3').list_buckets()['Buckets'])))
    print(util.colors.green('green means go'))
    print(os.environ['SOME_VAR'])
    print(event)
    return event
