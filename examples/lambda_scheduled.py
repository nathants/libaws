#!/usr/bin/env python3.6
#
# policy: CloudWatchLogsFullAccess
# trigger: cloudwatch cron(* * * * ? *) # or: cloudwatch rate(1 minute)

import pprint

def main(event, context):
    """
    >>> import shell
    >>> run = lambda *a, **kw: shell.run(*a, stream=True, **kw)
    >>> path = 'examples/lambda_scheduled.py'

    >>> run(f'aws-lambda-deploy {path} -y').split(':')[-1]
    'lambda-scheduled'

    >>> run(f'aws-lambda-logs {path} -f -e "scheduled trigger" | tail -n1').split('scheduled ')[-1]
    'trigger:'

    """
    print('scheduled trigger:\n' + pprint.pformat(event))
