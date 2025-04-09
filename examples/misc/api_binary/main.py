#!/usr/bin/env python3

import base64

def main(event, context):
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
