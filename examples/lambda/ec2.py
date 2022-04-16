#!/usr/bin/env python3
#
# attr: concurrency 0
# attr: memory 1024
# attr: timeout 900
# require: git+https://github.com/nathants/py-util
# require: git+https://github.com/nathants/py-pool
# require: git+https://github.com/nathants/py-shell
# require: git+https://github.com/nathants/cli-aws
# policy: AWSLambdaBasicExecutionRole
# allow: ec2:Describe* *
# allow: ec2:RunInstances *
# allow: ec2:CreateTags *

import shell as sh
import os

def main(event, context):
    """
    >>> import shell, uuid, json
    >>> run = lambda *a, **kw: shell.run(*a, stream=True, **kw)
    >>> path = __file__
    >>> uid = str(uuid.uuid4())[-12:]

    >>> _ = run(f'cli-aws lambda-ensure -y {path} $(env | grep -e ^AWS_EC2_KEY -e ^AWS_EC2_VPC -e ^AWS_EC2_SG)')

    >>> _ = run(f'cli-aws lambda-invoke {path} -es', "'%s'" % json.dumps({"uid": uid})).replace('"', '')

    >>> _ = run(f'for i in {{1..300}}; do sleep 1; cli-aws ec2-ls {uid} && break; done')

    >>> _ = run('cli-aws ec2-wait-for-ssh -y', uid)

    >>> _ = run('cli-aws ec2-ssh', uid, '-yc "for i in {1..60}; do ls /tmp/name.txt && break; sleep 1; done"')

    >>> assert uid == run(f'cli-aws ec2-ssh {uid} -yc "cat /tmp/name.txt"')

    >>> _ = shell.run('cli-aws ec2-rm -y', uid)

    >>> _ = run('cli-aws lambda-rm -ey', path)
    """

    name = event['uid']
    os.environ['PATH'] += f':{os.getcwd()}'
    instance_id = sh.run('cli-aws ec2-new',
                         name,
                         '--ami arch',
                         '--type t3.nano',
                         '--seconds-timeout 300',
                         '--no-wait',
                         '--init', f'"echo {name} > /tmp/name.txt"',
                         '2>&1',
                         stream=True)
    return instance_id
