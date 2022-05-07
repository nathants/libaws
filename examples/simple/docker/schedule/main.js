"use strict";

exports.handler = async (event, context) => {
  console.log(process.env['uid']);
  context.succeed();
}
