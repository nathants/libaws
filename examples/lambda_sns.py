#!/usr/bin/env python3.6
#
# policy: CloudWatchLogsFullAccess
# trigger: sns

def main(event, context):
    """
    >> aws-lambda-deploy examples/lambda_sns.py --yes

    >> aws sns publish --topic-arn $(aws-lambda-sns examples/lambda_sns.py) --message 'on the wire'

    >> aws-lambda-logs examples/lambda_sns.py -f

       log group: /aws/lambda/lambda-sns
       log stream: 2018/03/03/[$LATEST]fed20e45c1cb4b5d93bfffd7ad08d5ed
       2018-03-03 09:02:03.457000 START RequestId: 984b9323-1f04-11e8-a890-953e9bdbb348 Version: $LATEST
       2018-03-03 09:02:03.477000 thanks for: on the wire
       2018-03-03 09:02:03.477000 END RequestId: 984b9323-1f04-11e8-a890-953e9bdbb348
       2018-03-03 09:02:03.477000 REPORT RequestId: 984b9323-1f04-11e8-a890-953e9bdbb348
    """
    msg = event['Records'][0]['Sns']['Message']
    print('thanks for:', msg)
