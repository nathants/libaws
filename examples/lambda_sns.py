#!/usr/bin/env python3.6
#
# policy: CloudWatchLogsFullAccess
# trigger: sns

def main(event, context):
    """
    >>> import shell
    >>> run = lambda *a, **kw: shell.run(*a, stream=True, **kw)
    >>> path = 'examples/lambda_sns.py'

    >>> run(f'aws-lambda-deploy {path} -y').split(':')[-1]
    'lambda-sns'

    >>> run(f"aws sns publish --topic-arn $(aws-lambda-sns {path}) --message 'on the wire' >/dev/null")
    ''

    >>> run(f'aws-lambda-logs {path} -f -e "thanks for" | tail -n1').split('thanks for: ')[-1]
    'on the wire'

    """
    msg = event['Records'][0]['Sns']['Message']
    print('thanks for:', msg)
