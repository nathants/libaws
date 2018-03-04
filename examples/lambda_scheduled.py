#!/usr/bin/env python3.6
#
# policy: CloudWatchLogsFullAccess
# trigger: cloudwatch cron(* * * * ? *) # or: cloudwatch rate(1 minute)

import pprint

def main(event, context):
    """
    >>> import shell, json

    >>> run = lambda *a, **kw: shell.run(*a, stream=True, **kw)

    >>> run('aws-lambda-deploy examples/lambda_scheduled.py --yes').split(':')[-1]
    'lambda-scheduled'

    >>> ' '.join(run('aws-lambda-logs examples/lambda_scheduled.py -f -n 4 | grep "scheduled trigger"').split()[2:4])
    'scheduled trigger:'

    >>> run('aws-lambda-rm examples/lambda_scheduled.py')
    ''

    """
    print('scheduled trigger:', pprint.pformat(event))
