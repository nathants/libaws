#!/usr/bin/env python3.7
#
# policy: CloudWatchLogsFullAccess
# trigger: api
# trigger: cloudwatch rate(15 minutes) # keep lambda warm for fast responses

import json

def main(event, context):
    """
    >>> import shell, uuid
    >>> run = lambda *a, **kw: shell.run(*a, stream=True, **kw)
    >>> path = 'examples/lambda_api.py'
    >>> uid = str(uuid.uuid4())

    >>> _ = run(f'aws-lambda-deploy {path} -y')

    >>> api = run('aws-lambda-api', path)

    >>> assert '{"GET": null}' == run(f'curl {api} 2>/dev/null')

    >>> assert '{"POST": "%s"}' % uid == run(f'curl {api} -d {uid} 2>/dev/null')

    """
    return {'statusCode': '200',
            'isBase64Encoded': False,
            'headers': {'Content-Type': 'application/json'},
            'body': json.dumps({event['httpMethod']: event['body']})}
