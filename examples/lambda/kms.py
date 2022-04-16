#!/usr/bin/env python3
#
# attr: concurrency 0
# attr: memory 128
# attr: timeout 60
# policy: AWSLambdaBasicExecutionRole

import base64
import boto3
import os

def decrypt(text):
    return boto3.client('kms').decrypt(CiphertextBlob=base64.b64decode(bytes(text, 'utf-8')))['Plaintext'].decode('utf-8')

def main(event, context):
    """
    >>> import shell, uuid
    >>> run = lambda *a, **kw: shell.run(*a, stream=True, **kw)
    >>> path = __file__
    >>> uid = str(uuid.uuid4())[-12:]

    >>> _ = run(f'cli-aws lambda-rm -ey {path}')

    >>> _ = run(f'cli-aws lambda-ensure {path} UUID={uid} -y --kms && sleep 5 # iam is slow')

    >>> assert f'"{uid}"' == run('cli-aws lambda-invoke', path)

    >>> _ = run('cli-aws lambda-rm -ey', path)

    """
    return decrypt(os.environ['UUID'])
