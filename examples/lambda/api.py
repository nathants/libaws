#!/usr/bin/env python3
#
# conf: concurrency 0
# conf: memory 128
# conf: timeout 60
# policy: AWSLambdaBasicExecutionRole
# trigger: api
# trigger: cloudwatch rate(15 minutes) # keep lambda warm for fast responses

import json
import base64

def main(event, context):
    """
    >>> import shell, uuid
    >>> run = lambda *a, **kw: shell.run(*a, stream=True, **kw)
    >>> path = __file__
    >>> uid = str(uuid.uuid4())[-12:]

    >>> _ = run(f'aws-lambda-rm -ey {path}')

    >>> _ = run(f'aws-lambda-deploy {path} -y && sleep 5 # iam is slow')

    >>> api = run('aws-lambda-api', path)

    >>> _ = run(f'for i in $(seq 10); do curl -f {api} 2>/dev/null && exit 0; sleep 1; done; exit 1')

    >>> assert '["GET", "/", null]' == run(f'curl {api} 2>/dev/null')

    >>> assert '["POST", "/foo", "%s"]' % uid == run(f'curl {api}/foo -d {uid} 2>/dev/null')

    >>> _ = run('aws-lambda-rm -ey', path)

    """
    body = event['body']
    if body:
        body = base64.b64decode(bytes(body, 'utf-8')).decode()
    return {'statusCode': '200',
            'isBase64Encoded': False,
            'headers': {'Content-Type': 'application/json'},
            'body': json.dumps([event['httpMethod'], event['path'], "hello yallasdfasdfasdf"])}
