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
    """
      >> aws-lambda-deploy examples/lambda_basic.py SOME_VAR=some_val --yes

      >> echo '{"foo": "bar"}' > /tmp/input

      >> aws-lambda-invoke examples/lambda_basic.py --payload /tmp/input
         {'foo': 'bar'}

      >> aws-lambda-logs examples/lambda_basic.py -f

         log group: /aws/lambda/lambda-deploy
         log stream: 2018/03/02/[$LATEST]da996d15b51941a69d76b2c8acc6a73d
         2018-03-02 00:08:49.491000 START RequestId: efd7e55a-1df0-11e8-bdd2-d17e9059f5d9 Version: $LATEST
         2018-03-02 00:08:49.492000 log some stuff about requests: <function get at 0x7fc0c79176a8>
         2018-03-02 00:08:50.350000 you have some buckets: 4
         2018-03-02 00:08:50.375000 green means go
         2018-03-02 00:08:50.375000 some_val
         2018-03-02 00:08:50.375000 {'foo': 'bar'}
         2018-03-02 00:08:50.375000 END RequestId: efd7e55a-1df0-11e8-bdd2-d17e9059f5d9
         2018-03-02 00:08:50.375000 REPORT RequestId: efd7e55a-1df0-11e8-bdd2-d17e9059f5d9
    """
    print('log some stuff about requests:', requests.get)
    print('you have some buckets:', len(list(boto3.client('s3').list_buckets()['Buckets'])))
    print(util.colors.green('green means go'))
    print(os.environ['SOME_VAR'])
    print(event)
    return str(event)
