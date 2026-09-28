'use strict';

/** @type {import('aws-lambda').SQSHandler} */
exports.handler = async event => {
    for (const record of event.Records) {
        console.log('thanks for:', JSON.stringify(record));
    }
};
