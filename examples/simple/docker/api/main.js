"use strict";

exports.handler = async (event, context) => {
  let response = {
    statusCode: '200',
    body: 'ok\n',
    headers: {}
  };
  context.succeed(response);
}
