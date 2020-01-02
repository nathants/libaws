#!/usr/bin/env python3.8
#
# policy: CloudWatchLogsFullAccess
# trigger: sns

def main(event, context):
    """
    >>> import shell, uuid
    >>> run = lambda *a, **kw: shell.run(*a, stream=True, **kw)
    >>> path = 'examples/lambda/sns.py'
    >>> uid = str(uuid.uuid4())

    >>> _ = run(f'aws-lambda-deploy {path} -y')

    >>> _ = run(f"aws sns publish --topic-arn $(aws-lambda-sns {path}) --message {uid} >/dev/null")

    >>> assert uid == run(f'aws-lambda-logs {path} -f -e {uid} | tail -n1').split()[-1]

    """
    msg = event['Records'][0]['Sns']['Message']
    print(msg)
