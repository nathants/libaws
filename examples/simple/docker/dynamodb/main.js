'use strict';

const {DynamoDBClient, GetItemCommand, PutItemCommand} = require('@aws-sdk/client-dynamodb');

const client = new DynamoDBClient({region: process.env.AWS_DEFAULT_REGION});

/** @type {import('aws-lambda').DynamoDBStreamHandler} */
exports.handler = async event => {
    for (const record of event.Records) {
        let operation = 'GetItem';
        try {
            const res = await client.send(
                new GetItemCommand({
                    TableName: 'test-table-' + process.env.uid,
                    // This table has string and number keys. Binary stream keys
                    // would need their base64 decoded before passing them to the SDK.
                    Key: /** @type {Record<string, import('@aws-sdk/client-dynamodb').AttributeValue>} */ (record.dynamodb?.Keys),
                    ConsistentRead: true,
                })
            );
            if (!res.Item) {
                throw new Error('source item no longer exists');
            }
            operation = 'PutItem';
            await client.send(
                new PutItemCommand({
                    TableName: 'test-other-table-' + process.env.uid,
                    Item: res.Item,
                })
            );
        } catch (error) {
            console.error(
                JSON.stringify({
                    message: 'dynamodb record failed',
                    event_id: record.eventID,
                    source_arn: record.eventSourceARN,
                    operation,
                    error: error instanceof Error ? error.message : String(error),
                })
            );
            // Reject the invocation so Lambda counts the error and retries it.
            throw error;
        }
        console.info(JSON.stringify({message: 'dynamodb record copied', event_id: record.eventID}));
    }
};
