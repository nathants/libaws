#!/usr/bin/env python3
#
# conf: concurrency 0
# conf: memory 128
# conf: timeout 60
# policy: AWSLambdaBasicExecutionRole
# policy: AWSLambdaDynamoDBExecutionRole
# allow: dynamodb:GetItem arn:aws:dynamodb:*:*:table/test-table
# allow: dynamodb:PutItem arn:aws:dynamodb:*:*:table/test-other-table
# dynamodb: test-table userid:s:hash version:n:range stream=keys_only
# dynamodb: test-other-table userid:s:hash
# trigger: dynamodb test-table start=trim_horizon batch=1 parallel=10 retry=5

import boto3

dynamodb = boto3.client('dynamodb')

def main(event, context):
    """
    >>> import shell, uuid, json
    >>> run = lambda *a, **kw: shell.run(*a, stream=True, **kw)
    >>> path = __file__
    >>> uid = str(uuid.uuid4())[-12:]

    >>> _ = run(f'aws-lambda-rm -ey {path}')

    >>> _ = run(f'aws-lambda-deploy -y {path} && sleep 5 # iam is slow')

    >>> _ = run(f'aws-dynamodb-put test-table userid:s:jane version:n:1 data:s:{uid}')

    >>> _ = run(f'aws-lambda-logs {path} -f -e "put:"')

    >>> assert uid == json.loads(run('aws-dynamodb-get test-other-table userid:s:jane'))['data']

    >>> _ = run('aws-lambda-rm -ey', path)

    """

    for record in event['Records']:
        source_arn = record['eventSourceARN']
        _, table, *_ = source_arn.split('/')
        keys = record['dynamodb']['Keys']
        item = dynamodb.get_item(TableName=table, Key=keys)['Item']
        new_item = {}
        new_item['userid'] = item['userid']
        new_item['data'] = item['data']
        dynamodb.put_item(TableName='test-other-table', Item=new_item)
        print('got:', item)
        print('put:', new_item)
