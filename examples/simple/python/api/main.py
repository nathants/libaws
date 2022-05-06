#!/usr/bin/env python3

import json
import base64

def main(event, context):
    body = event['body']
    if body:
        body = base64.b64decode(bytes(body, 'utf-8')).decode()
    return {'statusCode': '200',
            'isBase64Encoded': False,
            'headers': {'Content-Type': 'application/json'},
            'body': json.dumps([event['httpMethod'], event['path'], body])}
