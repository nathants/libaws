'use strict';

/** @type {import('aws-lambda').S3Handler} */
exports.handler = async event => {
    for (const record of event.Records) {
        console.log(record.s3.object.key);
    }
};
