#!/usr/bin/env python3
#
# conf: concurrency 0
# conf: memory 128
# conf: timeout 900
# require: git+https://github.com/nathants/py-util
# require: git+https://github.com/nathants/py-pool
# require: git+https://github.com/nathants/py-shell
# require: git+https://github.com/nathants/cli-aws
# policy: AWSLambdaBasicExecutionRole
# allow: ec2:* *

import shell as sh
import os

def main(event, context):
    """
    >>> import shell, uuid, json
    >>> run = lambda *a, **kw: shell.run(*a, stream=True, **kw)
    >>> path = __file__
    >>> uid = str(uuid.uuid4())[-12:]

    >>> _ = run(f'aws-lambda-deploy -y {path} $(env | grep -e ^AWS_EC2_KEY -e ^AWS_EC2_VPC -e ^AWS_EC2_SG)')

    >>> instance_id = run(f'aws-lambda-invoke {path} -s', "'%s'" % json.dumps({"uid": uid})).replace('"', '')

    >>> _ = run('aws-ec2-wait-for-ssh -y', instance_id)

    >>> _ = run('aws-ec2-ssh', instance_id, '-yc "for i in {1..60}; do ls /tmp/name.txt && break; sleep 1; done"')

    >>> assert uid == run(f'aws-ec2-ssh {instance_id} -yc "cat /tmp/name.txt"')

    >>> _ = shell.run('aws-ec2-rm -y', instance_id)

    >>> _ = run('aws-lambda-rm -ey', path)
    """

    name = event['uid']
    os.environ['PATH'] += f':{os.getcwd()}'
    instance_id = sh.run('aws-ec2-new',
                         name,
                         '--ami arch',
                         '--type t3.nano',
                         '--seconds-timeout 300',
                         '--no-wait',
                         '--init', f'"echo {name} > /tmp/name.txt"',
                         '2>&1',
                         stream=True)
    return instance_id
