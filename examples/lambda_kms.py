#!/usr/bin/env python3.6
#
# require: git+https://github.com/nathants/py-util@e4aafbbb0f6e1bea793791356636968bef1924a2
# require: requests==2.18.4
# policy: CloudWatchLogsFullAccess
# allow: s3:List* *

import base64
import boto3
import os

def _decrypt(text):
    return boto3.client('kms').decrypt(CiphertextBlob=base64.b64decode(bytes(text, 'utf-8')))['Plaintext'].decode('utf-8')

def main(event, context):
    """
    >>> import shell, uuid
    >>> run = lambda *a, **kw: shell.run(*a, stream=True, **kw)
    >>> path = 'examples/lambda_kms.py'
    >>> uid = str(uuid.uuid4())

    >>> run(f'aws-lambda-deploy {path} SOME_UUID={uid} -y --kms').split(':')[-1]
    'lambda-kms'

    >>> assert f'"{uid}"' == run(f'aws-lambda-invoke {path}')

    >>> assert uid == run(f'aws-lambda-logs {path} -f -e {uid} | tail -n1').split()[-1]

    """
    val = _decrypt(os.environ['SOME_UUID'])
    print(val)
    return val
