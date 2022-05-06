"use strict";

exports.handler = async (event, context) => {
  for (const record of event['Records']) {
    console.log(record['s3']['object']['key']);
  }
  context.succeed();
}
