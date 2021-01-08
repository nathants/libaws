#!/usr/bin/env python3
#
# conf: concurrency 0
# conf: memory 128
# conf: timeout 60
# policy: AWSLambdaBasicExecutionRole
# trigger: api
# trigger: cloudwatch rate(15 minutes) # keep lambda warm for fast responses

import base64

def main(event, context):
    """
    >>> import shell, uuid
    >>> run = lambda *a, **kw: shell.run(*a, stream=True, **kw)
    >>> path = __file__
    >>> infile = run("mktemp")
    >>> outfile = run("mktemp")

    >>> _ = run("head -c 64 /dev/urandom >", infile)

    >>> _ = run(f'aws-lambda-rm -ey {path}')

    >>> _ = run(f'aws-lambda-deploy {path} -y && sleep 5 # iam is slow')

    >>> api = run('aws-lambda-api', path)

    >>> _ = run(f'for i in $(seq 10); do curl -f {api} 2>/dev/null && exit 0; sleep 1; done; exit 1')

    >>> _ = run(f'curl {api}/foo --data-binary @{infile} >{outfile} 2>/dev/null')

    >>> _ = run(f'[ "$(cat {infile} | md5sum)" = "$(cat {outfile} | md5sum)" ]')

    >>> _ = run('aws-lambda-rm -ey', path)

    >>> _ = run("rm", infile, outfile)

    """
    body = event.get('body')
    if body:
        # binary request body, when available, must be decoded
        body = base64.b64decode(body)
        assert len(body) == 64
    return {'statusCode': '200',
            'isBase64Encoded': True,
            'headers': {'Content-Type': 'application/octet-stream'},
            # binary response body, when returned, must be encoded
            'body': base64.b64encode(body).decode() if body else None}
