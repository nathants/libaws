'use strict';

/** @type {import('aws-lambda').ScheduledHandler} */
exports.handler = async () => {
    console.log(process.env['uid']);
};
