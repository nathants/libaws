#!/usr/bin/env python3
#
# attr: concurrency 0
# attr: memory 128
# attr: timeout 60
# policy: AWSLambdaBasicExecutionRole
# s3: ${bucket}
# trigger: s3 ${bucket}

import boto3

s3 = boto3.client('s3')

def main(event, context):
    """
    >>> import shell, uuid
    >>> run = lambda *a, **kw: shell.run(*a, stream=True, **kw)
    >>> path = __file__
    >>> bucket = f'cli-aws-{str(uuid.uuid4())[-12:]}'
    >>> uid = str(uuid.uuid4())[-12:]

    >>> _ = run(f'cli-aws lambda-rm -ey {path}')

    >>> _ = run(f'bucket={bucket} cli-aws lambda-ensure -y {path} && sleep 5 # iam is slow')

    >>> _ = run(f'echo | aws s3 cp - s3://{bucket}/{uid}')

    >>> assert uid == run(f'cli-aws lambda-logs {path} -f -e {uid} | tail -n1').split()[-1]

    >>> _ = run(f'bucket={bucket} cli-aws lambda-rm -ey', path)

    """
    for record in event['Records']:
        print(record['s3']['object']['key'])
