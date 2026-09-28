'use strict';

/** @type {import('aws-lambda').EventBridgeHandler<string, unknown, void>} */
exports.handler = async event => {
    console.log('got:', JSON.stringify(event));
};
