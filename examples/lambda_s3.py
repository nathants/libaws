#!/usr/bin/env python3.7
#
# policy: CloudWatchLogsFullAccess
# trigger: s3 cli-aws-s3-test-bucket
#

def main(event, context):
    """
    >>> import shell, uuid
    >>> run = lambda *a, **kw: shell.run(*a, stream=True, **kw)
    >>> path = 'examples/lambda_s3.py'
    >>> bucket = 'cli-aws-s3-test-bucket'
    >>> uid = str(uuid.uuid4())

    >>> _ = run(f'aws s3 rm s3://{bucket} --recursive || true')
    >>> _ = run(f'aws s3 rb s3://{bucket} || true')
    >>> _ = run(f'aws s3 mb s3://{bucket}')

    >>> _ = run(f'aws-lambda-deploy {path} -y')

    >>> _ = run(f'echo | aws s3 cp - s3://{bucket}/{uid}')

    >>> assert uid == run(f'aws-lambda-logs {path} -f -e {uid} | tail -n1').split()[-1]

    """
    for record in event['Records']:
        print(record['s3']['object']['key'])
