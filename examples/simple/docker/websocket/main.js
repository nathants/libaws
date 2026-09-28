'use strict';

/** @type {import('aws-lambda').APIGatewayProxyWebsocketHandlerV2} */
exports.handler = async () => {
    return {
        statusCode: 200,
        body: 'ok\n',
        headers: {},
    };
};
