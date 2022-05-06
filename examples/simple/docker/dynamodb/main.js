"use strict";

const { DynamoDBClient, GetItemCommand, PutItemCommand } = require("@aws-sdk/client-dynamodb");

exports.handler = async (event, context) => {
  try {
    const client = new DynamoDBClient({ region: process.env.AWS_DEFAULT_REGION });
    for (const record of event['Records']) {
      const source_arn = record['eventSourceARN'];
      const table = source_arn.split('/')[1];
      const getCommand = new GetItemCommand({
        TableName: 'test-table-' + process.env.uid,
        Key: record['dynamodb']['Keys']
      });
      const res = await client.send(getCommand);
      const item = res['Item'];
      const putCommand = new PutItemCommand({
        TableName: 'test-other-table-' + process.env.uid,
        Item: item
      });
      const putRes = await client.send(putCommand);
      console.log('put:', JSON.stringify(item));
    }
  } catch (err) {
    console.error(err);
  }
  context.succeed();
}
