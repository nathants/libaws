'use strict';

/** @type {import('aws-lambda').APIGatewayProxyWebsocketHandlerV2} */
exports.handler = async () => {
    //
    // TODO
    //
    // this test currently just checks that the websocket trigger can be successfully deployed.
    //
    // for websocket example see: https://github.com/nathants/aws-gocljs
    //
    return {
        statusCode: 200,
        body: 'ok\n',
        headers: {},
    };
};
