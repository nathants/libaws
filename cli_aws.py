import boto3
import shell
import datetime
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

lambda_stage_name = 'main'

def subnet(vpc, zone):
    vpcs = list(boto3.resource('ec2').vpcs.filter(Filters=[{'Name': 'vpc-id' if vpc.startswith('vpc-') else 'tag:Name', 'Values': [vpc]}]))
    assert len(vpcs) == 1, 'no vpc named: %s' % vpc
    if zone:
        subnets = [x for x in vpcs[0].subnets.all() if x.availability_zone == zone]
    else:
        subnets = list(vpcs[0].subnets.all())[:1]
    assert len(subnets) == 1, 'no subnet for vpc=%(vpc)s zone=%(zone)s' % locals()
    subnet = subnets[0].id
    logging.info(f'using zone: {zone}, subnet: {subnet}, vpn: {vpc}')
    return subnet

@retry
def sgs(names=None):
    sgs = list(boto3.resource('ec2').security_groups.all())
    if names:
        sgs = [x
               for x in sgs
               if x.group_name in names
               or x.group_id in names]
    return sgs

def now():
    return str(datetime.datetime.utcnow().isoformat()) + 'Z'

def regions():
    return [x['RegionName'] for x in boto3.client('ec2').describe_regions()['Regions']]

def zones():
    return [x['ZoneName'] for x in boto3.client('ec2').describe_availability_zones()['AvailabilityZones']]

def ssh_user(*instances):
    try:
        users = {tags(i)['ssh-user'] for i in instances}
    except KeyError:
        assert False, 'instances should have a tag "user=<username>"'
    assert len(users), 'no user tag found: %s' % '\n '.join(format(i) for i in instances)
    assert len(users) == 1, 'cannot operate on instances with heterogeneous users: %s' % users
    return users.pop()

def tags(obj):
    if isinstance(obj, dict):
        tags = obj.get('Tags') or {}
    else:
        tags = obj.tags or {}
    return {x['Key']: x['Value'].replace('\t', '_').replace(' ', '_') for x in tags}

def ec2_name(instance):
    return tags(instance).get('Name', '<no-name>')

_hidden_tags = [
    'Name',
    'user',
    'creation-date',
    'owner',
    'aws:ec2spot:fleet-request-id',
    'aws:elasticmapreduce:job-flow-id',
]

def format(i, all_tags=False):
    return ' '.join([(green if i.state['Name'] == 'running' else cyan if i.state['Name'] == 'pending' else red)(ec2_name(i)),
                     i.instance_type,
                     i.state['Name'],
                     i.instance_id,
                     i.image_id,
                     ('spot' if i.spot_instance_request_id else 'ondemand'),
                     ','.join(sorted([x['GroupName'] for x in i.security_groups])),
                     ' '.join('%s=%s' % (k, v) for k, v in sorted(tags(i).items(), key=lambda x: x[0]) if (all_tags or k not in _hidden_tags) and v)])

def ls(selectors, state):
    assert state in ['running', 'pending', 'stopped', 'terminated', None], f'bad state: {state}'
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
        kind = 'private-ip-address' if all(x.isdigit() or x == '.' for x in selectors[0]) and selectors[0].split('.')[0] in ['10', '172'] else kind
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
    if util.misc.override('--stream'):
        shell.set['stream'] = 'yes'
    try:
        yield
    except AssertionError as e:
        logging.info(red('error: %s' % (e.args[0] if e.args else traceback.format_exc().splitlines()[-2].strip())))
        sys.exit(1)
    except:
        raise

def parse_metadata(token, xs, silent=False):
    vals = [x.split(token, 1)[-1].split('#')[0].strip()
            for x in xs
            if x.strip().startswith('#')
            and token in x]
    silent or print(token, file=sys.stderr)
    for val in vals:
        silent or print('', val, file=sys.stderr)
    silent or print(file=sys.stderr)
    return vals

def lambda_arn(name):
    return boto3.client('lambda').get_function(FunctionName=name)['Configuration']['FunctionArn']

def lambda_name(path):
    if not os.path.isfile(path):
        print('no such file:', path, file=sys.stderr)
        sys.exit(1)
    name = os.path.basename(path).replace(' ', '-').replace('_', '-').split('.py')[0]
    assert '.' not in name, '"." should not be in your filepath except for a file extension'
    return name

def region():
    boto3.client('ec2') # run session setup logic
    return boto3.DEFAULT_SESSION.region_name

def rest_apis(name=None):
    for page in boto3.client('apigateway').get_paginator('get_rest_apis').paginate():
        for item in page['items']:
            if not name or item['name'] == name:
                yield item['name'], item['id'], ','.join(item['endpointConfiguration']['types']), item['createdDate']

def rest_api_id(name):
    apis = []
    for _, id, *_ in rest_apis(name):
        apis.append(id)
    assert len(apis) == 1, f'didnt find exactly 1 api for name: {name}, {apis}'
    return apis[0]

def rest_resource_id(rest_api_id, path):
    for page in boto3.client('apigateway').get_paginator('get_resources').paginate(restApiId=rest_api_id):
        for item in page['items']:
            if item['path'] == path:
                return item['id']

def account():
    return boto3.client('sts').get_caller_identity()['Account']

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


def ensure_allows(name, allows):
    print('\nensure allows', file=sys.stderr)
    for allow in allows:
        action, resource = allow.split()
        print(' ensure allow:', allow, file=sys.stderr)
        policy = f'''{{"Version": "2012-10-17",
                       "Statement": [{{"Effect": "Allow",
                                       "Action": "{action}",
                                       "Resource": "{resource}"}}]}}'''
        boto3.client('iam').put_role_policy(RoleName=name, PolicyName=_policy_name(allow), PolicyDocument=policy)

def ensure_policies(name, policies):
    print('\nensure policies', file=sys.stderr)
    all_policies = [policy for page in boto3.client('iam').get_paginator('list_policies').paginate() for policy in page['Policies']]
    for policy in policies:
        matched_polices = [x for x in all_policies if x['Arn'].split('/')[-1] == policy]
        if 0 == len(matched_polices):
            print(' didnt find any policy:', policy, file=sys.stderr)
            sys.exit(1)
        elif 1 == len(matched_polices):
            boto3.client('iam').attach_role_policy(RoleName=name, PolicyArn=matched_polices[0]["Arn"])
            print(' attached policy:', policy, file=sys.stderr)
        else:
            print(' found more than 1 policy:', policy, file=sys.stderr)
            for p in matched_polices:
                print(p['Arn'], file=sys.stderr)
            sys.exit(1)

def ensure_role(name, principal):
    print('ensure role', file=sys.stderr)
    role_path = f'/{principal}/{name}-path/'
    roles = [role for page in boto3.client('iam').get_paginator('list_roles').paginate(PathPrefix=role_path) for role in page['Roles']]
    if 0 == len(roles):
        print(' create role:', name, file=sys.stderr)
        policy = '''{"Version": "2012-10-17",
                     "Statement": [{"Effect": "Allow",
                                    "Principal": {"Service": "%s.amazonaws.com"},
                                    "Action": "sts:AssumeRole"}]}''' % principal
        return boto3.client('iam').create_role(Path=role_path, RoleName=name, AssumeRolePolicyDocument=policy)['Role']['Arn']
    elif 1 == len(roles):
        print(' role exists:', name, file=sys.stderr)
        return roles[0]['Arn']
    else:
        print(' error: there is more than 1 role under path:', role_path, file=sys.stderr)
        for role in roles:
            print('', role, file=sys.stderr)
        sys.exit(1)

def ensure_instance_profile(name, role_name):
    print('\nensure instance profile', file=sys.stderr)
    profiles = [profile
                for page in boto3.client('iam').get_paginator('list_instance_profiles').paginate()
                for profile in page['InstanceProfiles']
                if profile['InstanceProfileName'] == name]
    if 0 == len(profiles):
        print(' create instance profile:', name, file=sys.stderr)
        profile = boto3.client('iam').create_instance_profile(InstanceProfileName=name)['InstanceProfile']
    elif 1 == len(profiles):
        print(' profile exists:', name, file=sys.stderr)
        profile = profiles[0]
    else:
        assert False, profiles
    roles = [role['RoleName'] for role in profile['Roles']]
    if role_name not in roles:
        print(' add role to instance profile:', role_name, file=sys.stderr)
        boto3.client('iam').add_role_to_instance_profile(InstanceProfileName=name, RoleName=role_name)
    else:
        print(' role already added to instance profile:', role_name, file=sys.stderr)
    return profile['Arn']

def _policy_name(allow):
    return allow.replace('*', 'All').replace(' ', '-').replace(':', '--')

def remove_extra_policies(name, policies):
    print('\nremove extra policies', file=sys.stderr)
    attached_role_policies = [policy for page in boto3.client('iam').get_paginator('list_attached_role_policies').paginate(RoleName=name) for policy in page['AttachedPolicies']]
    for policy in attached_role_policies:
        if policy['PolicyName'] not in policies:
            print('detaching policy:', policy['PolicyName'], file=sys.stderr)
            boto3.client('iam').detach_role_policy(RoleName=name, PolicyArn=policy["PolicyArn"])

def remove_extra_allows(name, allows):
    print('\nremove extra allows', file=sys.stderr)
    role_policies = [policy for page in boto3.client('iam').get_paginator('list_role_policies').paginate(RoleName=name) for policy in page['PolicyNames']]
    for policy in role_policies:
        if policy not in [_policy_name(x) for x in allows]:
            print('removing policy:', policy, file=sys.stderr)
            boto3.client('iam').delete_role_policy(RoleName=name, PolicyName=policy)
