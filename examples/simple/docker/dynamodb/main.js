'use strict';

const {DynamoDBClient, GetItemCommand, PutItemCommand} = require('@aws-sdk/client-dynamodb');

/** @type {import('aws-lambda').DynamoDBStreamHandler} */
exports.handler = async event => {
    try {
        const client = new DynamoDBClient({region: process.env.AWS_DEFAULT_REGION});
        for (const record of event.Records) {
            const getCommand = new GetItemCommand({
                TableName: 'test-table-' + process.env.uid,
                // Stream records carry keys in DynamoDB JSON, which the SDK sends unchanged for this
                // table's string and number keys. Binary keys would need their base64 decoded.
                Key: /** @type {Record<string, import('@aws-sdk/client-dynamodb').AttributeValue>} */ (record.dynamodb?.Keys),
            });
            const res = await client.send(getCommand);
            const item = res.Item;
            const putCommand = new PutItemCommand({
                TableName: 'test-other-table-' + process.env.uid,
                Item: item,
            });
            await client.send(putCommand);
            console.log('put:', JSON.stringify(item));
        }
    } catch (err) {
        console.error(err);
    }
};
