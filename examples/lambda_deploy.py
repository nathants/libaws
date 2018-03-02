#!/usr/bin/env python3.6
#
# require: git+https://github.com/nathants/py-util@e4aafbbb0f6e1bea793791356636968bef1924a2
# require: requests==2.18.4
# policy: CloudWatchLogsFullAccess
# allow: s3:List* *

import requests
import util.colors
import os
import boto3
import os

def main(event, context):
    print('log some stuff about requests:', requests.get)
    print('you have some buckets:', len(list(boto3.client('s3').list_buckets()['Buckets'])))
    print(util.colors.green('green means go'))
    print(os.environ['SOME_VAR'])
    return 'nailed it'
