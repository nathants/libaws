#!/usr/bin/env python3.6
#
# require: git+https://github.com/nathants/py-util@e4aafbbb0f6e1bea793791356636968bef1924a2
# require: requests==2.18.4
# policy: CloudWatchLogsFullAccess
# allow: s3:List* *

import requests
import util.colors
import os
import boto3
import os

def main(event, context):
    """
    >>> import shell

    >>> run = lambda *a, **kw: shell.run(*a, stream=True, **kw)

    >>> run('aws-lambda-deploy examples/lambda_basic.py SOME_VAR=some_val --yes').split(':')[-1]
    'lambda-basic'

    >>> run('cat - > /tmp/input', stdin='{"foo": "bar"}')
    ''
    >>> run('aws-lambda-invoke examples/lambda_basic.py --payload /tmp/input')
    '{"foo": "bar"}'

    >>> run('aws-lambda-logs examples/lambda_basic.py -f -n7 | grep some_val').split()[-1]
    'some_val'

    >>> run('aws-lambda-rm examples/lambda_basic.py')
    ''

    """
    print('log some stuff about requests:', requests.get)
    print('you have some buckets:', len(list(boto3.client('s3').list_buckets()['Buckets'])))
    print(util.colors.green('green means go'))
    print(os.environ['SOME_VAR'])
    print(event)
    return event
