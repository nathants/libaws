import boto3
import contextlib
import traceback
import logging
import os
import sys
import util.iter
import util.log
from util.colors import red, green, cyan
from util.retry import retry

ssh_args = ' -q -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no '

def ssh_user(*instances):
    try:
        users = {tags(i)['user'] for i in instances}
    except KeyError:
        assert False, 'instances should have a tag "user=<username>"'
    assert len(users), 'no user tag found: %s' % '\n '.join(format(i) for i in instances)
    assert len(users) == 1, 'cannot operate on instances with heteragenous users: %s' % users
    return users.pop()

def tags(instance):
    return {x['Key']: x['Value'].replace('\t', '_').replace(' ', '_') for x in (instance.tags or {})}

def name(instance):
    return tags(instance).get('Name', '<no-name>')

def format(i, all_tags=False):
    return ' '.join([(green if i.state['Name'] == 'running' else cyan if i.state['Name'] == 'pending' else red)(name(i)),
                     i.instance_type,
                     i.state['Name'],
                     i.instance_id,
                     i.image_id,
                     ('spot' if i.spot_instance_request_id else 'ondemand'),
                     ','.join(sorted([x['GroupName'] for x in i.security_groups])),
                     ' '.join('%s=%s' % (k, v) for k, v in sorted(tags(i).items(), key=lambda x: x[0]) if (all_tags or k not in ['Name', 'user', 'date', 'owner', 'aws:ec2spot:fleet-request-id']) and v)])

def ls(selectors, state):
    assert state in ['running', 'pending', 'stopped', 'terminated', None]
    if not selectors:
        instances = list(retry(boto3.resource('ec2').instances.filter)(Filters=[{'Name': 'instance-state-name', 'Values': [state]}] if state else []))
    else:
        kind = 'tags'
        kind = 'dns-name' if selectors[0].endswith('.amazonaws.com') else kind
        kind = 'vpc-id' if selectors[0].startswith('vpc-') else kind
        kind = 'subnet-id' if selectors[0].startswith('subnet-') else kind
        kind = 'instance.group-id' if selectors[0].startswith('sg-') else kind
        kind = 'private-dns-name' if selectors[0].endswith('.ec2.internal') else kind
        kind = 'ip-address' if all(x.isdigit() or x == '.' for x in selectors[0]) else kind
        kind = 'private-ip-address' if all(x.isdigit() or x == '.' for x in selectors[0]) and selectors[0].startswith('10.') else kind
        kind = 'instance-id' if selectors[0].startswith('i-') else kind
        if kind == 'tags' and '=' not in selectors[0]:
            selectors = f'Name={selectors[0]}', *selectors[1:] # auto add Name= to the first tag
        instances = []
        for chunk in util.iter.chunk(selectors, 195): # 200 boto api limit
            filters = [{'Name': 'instance-state-name', 'Values': [state]}] if state else []
            if kind == 'tags':
                filters += [{'Name': f'tag:{k}', 'Values': [v]} for t in chunk for k, v in [t.split('=')]]
            else:
                filters += [{'Name': kind, 'Values': chunk}]
            instances += list(retry(boto3.resource('ec2').instances.filter)(Filters=filters))
    instances = sorted(instances, key=lambda i: i.instance_id)
    instances = sorted(instances, key=lambda i: tags(i).get('name', 'no-name'))
    instances = sorted(instances, key=lambda i: i.meta.data['LaunchTime'], reverse=True)
    return instances

@contextlib.contextmanager
def setup():
    util.log.setup(format='%(message)s')
    logging.getLogger('botocore').setLevel('ERROR')
    if 'region' in os.environ:
        boto3.setup_default_session(region_name=os.environ['region'])
    elif 'REGION' in os.environ:
        boto3.setup_default_session(region_name=os.environ['REGION'])
    try:
        yield
    except AssertionError as e:
        logging.info(red(e.args[0] if e.args else traceback.format_exc().splitlines()[-2].strip()))
        sys.exit(1)
    except:
        raise

def lambda_name(path):
    return os.path.basename(path).replace(' ', '-').replace('_', '-').split('.py')[0]

def region():
    boto3.client('ec2') # run session setup logic
    return boto3.DEFAULT_SESSION.region_name

region_names = {
    "us-east-2": "US East (Ohio)",
    "us-east-1": "US East (N. Virginia)",
    "us-west-1": "US West (N. California)",
    "us-west-2": "US West (Oregon)",
    "ap-south-1": "Asia Pacific (Mumbai)",
    "ap-northeast-2": "Asia Pacific (Seoul)",
    "ap-southeast-1": "Asia Pacific (Singapore)",
    "ap-southeast-2": "Asia Pacific (Sydney)",
    "ap-northeast-1": "Asia Pacific (Tokyo)",
    "ca-central-1": "Canada (Central)",
    "cn-north-1": "China (Beijing)",
    "eu-central-1": "EU (Frankfurt)",
    "eu-west-1": "EU (Ireland)",
    "eu-west-2": "EU (London)",
    "eu-west-3": "EU (Paris)",
    "sa-east-1": "South America (São Paulo)",
}
