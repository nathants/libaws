"use strict";

exports.handler = async (event, context) => {
  console.log('got:', JSON.stringify(event));
  context.succeed();
}
