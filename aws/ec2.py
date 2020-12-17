from aws import retry, resource, stderr
from util.colors import red, green, cyan
import pprint
import util.iter

ssh_args = ' -q -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no '

@retry
def sgs(names=None):
    sgs = list(resource('ec2').security_groups.all())
    if names:
        sgs = [x
               for x in sgs
               if x.group_name in names
               or x.group_id in names]
    return sgs

def subnet(vpc, zone, print_fn=stderr):
    vpcs = list(resource('ec2').vpcs.filter(Filters=[{'Name': 'vpc-id' if vpc.startswith('vpc-') else 'tag:Name', 'Values': [vpc]}]))
    assert len(vpcs) == 1, 'no vpc named: %s' % vpc
    if zone:
        subnets = [x for x in vpcs[0].subnets.all() if x.availability_zone == zone]
    else:
        subnets = list(vpcs[0].subnets.all())[:1]
    assert len(subnets) == 1, 'no subnet for vpc=%(vpc)s zone=%(zone)s' % locals()
    subnet = subnets[0].id
    print_fn(f'using zone: {zone}, subnet: {subnet}, vpn: {vpc}')
    return subnet

def ssh_user(*instances):
    try:
        users = {tags(i)['user'] for i in instances}
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

def tag_name(instance, default='<no-name>'):
    return tags(instance).get('Name', default)

_hidden_tags = [
    'Name',
    'user',
    'creation-date',
    'owner',
    'STAGE',
]

def format_header(all_tags=False, placement=False):
    return ' '.join(filter(None, [
        'name',
        'type',
        'state',
        'id',
        'image-id',
        'kind',
        'security-group',
        'vpc' if placement else None,
        'zone' if placement else None,
        'tags...',
    ]))

def format(i, all_tags=False, placement=False, aws_tags=False):
    return ' '.join(filter(None, [
        (green if i.state['Name'] == 'running' else cyan if i.state['Name'] == 'pending' else red)(tag_name(i)),
        i.instance_type,
        i.state['Name'],
        i.instance_id,
        i.image_id,
        ('spot' if i.spot_instance_request_id else 'ondemand'),
        ','.join(sorted([x['GroupName'] for x in i.security_groups]) or ['-']),
        tag_name(i.vpc or {}, '-') if placement else None,
        (i.subnet.availability_zone if i.subnet else '-') if placement else None,
        ' '.join('%s=%s' % (k, v)
                 for k, v in sorted(tags(i).items(), key=lambda x: x[0])
                 if (all_tags or k not in _hidden_tags)
                 and (aws_tags or not k.startswith('aws:'))
                 and v),
    ]))

def ls(selectors, state):
    assert state in ['running', 'pending', 'stopped', 'terminated', None], f'bad state: {state}'
    if not selectors:
        instances = list(retry(resource('ec2').instances.filter)(Filters=[{'Name': 'instance-state-name', 'Values': [state]}] if state else []))
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
            # TODO allow passing multiple names, which are summed, and then tags, which are intersected
            selectors = f'Name={selectors[0]}', *selectors[1:] # auto add Name= to the first tag
        instances = []
        for chunk in util.iter.chunk(selectors, 195): # 200 boto api limit
            filters = [{'Name': 'instance-state-name', 'Values': [state]}] if state else []
            try:
                if kind == 'tags':
                    filters += [{'Name': f'tag:{k}', 'Values': [v]} for t in chunk for k, v in [t.split('=')]]
                else:
                    filters += [{'Name': kind, 'Values': chunk}]
            except:
                pprint.pprint(selectors)
                pprint.pprint(filters)
                raise
            instances += list(retry(resource('ec2').instances.filter)(Filters=filters))
    instances = sorted(instances, key=lambda i: i.instance_id)
    instances = sorted(instances, key=lambda i: tags(i).get('name', 'no-name'))
    instances = sorted(instances, key=lambda i: i.meta.data['LaunchTime'], reverse=True)
    return instances
