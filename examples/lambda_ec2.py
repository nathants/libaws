#!/usr/bin/env python3.6
#
# require: git+https://github.com/nathants/py-util
# require: git+https://github.com/nathants/py-pool
# require: git+https://github.com/nathants/py-shell
# require: git+https://github.com/nathants/cli-aws
# policy: CloudWatchLogsFullAccess
# allow: ec2:* *
# allow: pricing:* *
# allow: iam:* *

import shell
import os

def main(event, context):
    """
    >>> import shell, uuid
    >>> run = lambda *a, **kw: shell.run(*a, stream=True, **kw)
    >>> path = 'examples/lambda_ec2.py'
    >>> uid = str(uuid.uuid4())

    >>> _ = run(f'aws-lambda-deploy {path} UUID={uid} $(env | grep -i ^aws) -y').split(':')[-1]

    >>> instance_id = run(f'aws-lambda-invoke {path}').replace('"', '')

    >>> _ = run(f'aws-ec2-wait-for-ssh -y {instance_id}')

    >>> _ = run('aws-ec2-ssh', instance_id, '-yc "for i in {1..60}; do ls /tmp/name.txt && break; sleep 1; done"')

    >>> assert uid == run(f'aws-ec2-ssh {instance_id} -yc "cat /tmp/name.txt"')

    >>> _ = shell.run(f'aws-ec2-rm -y {instance_id}')

    """
    name = os.environ['UUID']
    os.environ['PATH'] += f':{os.getcwd()}'
    with shell.set_stream():
        instance_id = shell.run('aws-ec2-new',
                                name,
                                '--ami arch',
                                '--type t3.nano',
                                '--seconds-timeout 300',
                                '--role s3-all',
                                '--no-wait',
                                '--init', f'"echo {name} > /tmp/name.txt"')
        return instance_id
