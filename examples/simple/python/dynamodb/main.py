#!/usr/bin/env python3
import os
import boto3

dynamodb = boto3.client('dynamodb')

uid = os.environ['uid']

def main(event, context):
    for record in event['Records']:
        source_arn = record['eventSourceARN']
        _, table, *_ = source_arn.split('/')
        keys = record['dynamodb']['Keys']
        item = dynamodb.get_item(TableName=table, Key=keys)['Item']
        dynamodb.put_item(TableName=f'test-other-table-{uid}', Item=item)
        print('put:', item)
