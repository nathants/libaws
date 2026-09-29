#!/usr/bin/env python3
import json
import logging
import os

import boto3

dynamodb = boto3.client('dynamodb')
logger = logging.getLogger(__name__)
logger.setLevel(logging.INFO)
uid = os.environ['uid']


def main(event, context):
    for record in event['Records']:
        operation = 'GetItem'
        try:
            response = dynamodb.get_item(
                TableName=f'test-table-{uid}', Key=record['dynamodb']['Keys'], ConsistentRead=True,
            )
            item = response.get('Item')
            if not item:
                raise ValueError('source item no longer exists')
            operation = 'PutItem'
            dynamodb.put_item(TableName=f'test-other-table-{uid}', Item=item)
        except Exception as error:
            logger.error(json.dumps({
                'message': 'dynamodb record failed',
                'event_id': record.get('eventID'),
                'source_arn': record.get('eventSourceARN'),
                'operation': operation,
                'error': str(error),
            }))
            # Raise so Lambda counts the error and retries the invocation.
            raise
        logger.info(json.dumps({'message': 'dynamodb record copied', 'event_id': record.get('eventID')}))
