#!/usr/bin/env python3.6
#
# policy: CloudWatchLogsFullAccess
# trigger: sns

def main(event, context):
    """
    >>> import shell, json

    >>> run = lambda *a, **kw: shell.run(*a, stream=True, **kw)

    >>> run('aws-lambda-deploy examples/lambda_sns.py --yes').split(':')[-1]
    'lambda-sns'

    >>> json.loads(run("aws sns publish --topic-arn $(aws-lambda-sns examples/lambda_sns.py) --message 'on the wire'")).popitem()[0]
    'MessageId'

    >>> ' '.join(run('aws-lambda-logs examples/lambda_sns.py -f -n 4 | grep "thanks for"').split()[2:])
    'thanks for: on the wire'

    >>> run('aws-lambda-rm examples/lambda_sns.py')
    ''

    """
    msg = event['Records'][0]['Sns']['Message']
    print('thanks for:', msg)
