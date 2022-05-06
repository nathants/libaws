"use strict";

exports.handler = async (event, context) => {
  for (const record of event['Records']) {
    console.log('thanks for:', JSON.stringify(record));
  }
  context.succeed();
}
