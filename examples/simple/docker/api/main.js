'use strict';

/** @type {import('aws-lambda').APIGatewayProxyHandler} */
exports.handler = async () => {
    return {
        statusCode: 200,
        body: 'ok\n',
        headers: {},
    };
};
