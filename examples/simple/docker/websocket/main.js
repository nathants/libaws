"use strict";

exports.handler = async (event, context) => {
  //
  // TODO
  //
  // this test currently just checks that the websocket trigger can be successfully deployed.
  //
  // for websocket example see: https://github.com/nathants/new-gocljs
  //
  let response = {
    statusCode: '200',
    body: 'ok\n',
    headers: {}
  };
  context.succeed(response);
}
