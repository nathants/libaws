import sys
import aws
from aws import stderr, client

def ensure_allows(name, allows, preview):
    if allows:
        stderr('\nensure allows:')
        for allow in allows:
            action, resource = allow.split()
            if preview:
                stderr(' preview:', allow)
            else:
                stderr('', allow)
                policy = f'''{{"Version": "2012-10-17",
                               "Statement": [{{"Effect": "Allow",
                                               "Action": "{action}",
                                               "Resource": "{resource}"}}]}}'''
                client('iam').put_role_policy(RoleName=name, PolicyName=_policy_name(allow), PolicyDocument=policy)

def ensure_policies(name, policies, preview):
    if policies:
        stderr('\nensure policies:')
        if preview:
            all_policies = []
        else:
            all_policies = [policy for page in client('iam').get_paginator('list_policies').paginate() for policy in page['Policies']]
        for policy in policies:
            if preview:
                stderr(' preview:', policy)
            else:
                matched_polices = [x for x in all_policies if x['Arn'].split('/')[-1] == policy]
                if 0 == len(matched_polices):
                    stderr('fatal: didnt find any policy:', policy)
                    sys.exit(1)
                elif 1 == len(matched_polices):
                    client('iam').attach_role_policy(RoleName=name, PolicyArn=matched_polices[0]["Arn"])
                    stderr('', policy)
                else:
                    stderr('fatal: found more than 1 policy:', policy)
                    for p in matched_polices:
                        stderr(p['Arn'])
                    sys.exit(1)

def role_arn(name, principal):
    return f'arn:aws:iam::{aws.account()}:role/{principal}/{name}-path/{name}'

def ensure_role(name, principal, preview):
    stderr('\nensure role:')
    if preview:
        stderr(' preview:', name)
    else:
        role_path = f'/{principal}/{name}-path/'
        roles = [role for page in client('iam').get_paginator('list_roles').paginate(PathPrefix=role_path) for role in page['Roles']]
        if 0 == len(roles):
            stderr('', name)
            policy = '''{"Version": "2012-10-17",
                         "Statement": [{"Effect": "Allow",
                                        "Principal": {"Service": "%s.amazonaws.com"},
                                        "Action": "sts:AssumeRole"}]}''' % principal
            client('iam').create_role(Path=role_path, RoleName=name, AssumeRolePolicyDocument=policy)
        elif 1 == len(roles):
            stderr('', name)
        else:
            stderr(' error: there is more than 1 role under path:', role_path)
            for role in roles:
                stderr('', role)
            sys.exit(1)

def instance_profile_arn(name):
    return f'arn:aws:iam::{aws.account()}:instance-profile/{name}'

def ensure_instance_profile_has_role(name, role_name, preview):
    stderr('\nensure instance profile has role:')
    profiles = [profile
                for page in client('iam').get_paginator('list_instance_profiles').paginate()
                for profile in page['InstanceProfiles']
                if profile['InstanceProfileName'] == name]
    if 0 == len(profiles):
        if preview:
            stderr(' preview: created:', name)
            profile = None
        else:
            profile = client('iam').create_instance_profile(InstanceProfileName=name)['InstanceProfile']
            stderr(' created:', name)
    elif 1 == len(profiles):
        if preview:
            stderr(' preview: exists:', name)
        else:
            stderr(' exists:', name)
        profile = profiles[0]
    else:
        assert False, profiles
    if profile:
        roles = [role['RoleName'] for role in profile['Roles']]
        if role_name not in roles:
            client('iam').add_role_to_instance_profile(InstanceProfileName=name, RoleName=role_name)


def _policy_name(allow):
    action, resource = allow.split()
    action = action.replace('*', 'ALL')
    resource = resource.replace('*', 'ALL')
    resource = ':'.join([x.replace('/', '__') for x in resource.split(':') if x and x not in ['arn', 'aws', 's3', 'dynamodb', 'sqs']]) # arn:aws:service:account:region:target
    return f'{action}__{resource}'.replace(':', '_').rstrip('_')

def rm_extra_policies(name, policies, preview):
    to_remove = []
    try:
        attached_role_policies = [policy for page in client('iam').get_paginator('list_attached_role_policies').paginate(RoleName=name) for policy in page['AttachedPolicies']]
    except client('iam').exceptions.NoSuchEntityException:
        pass
    else:
        for policy in attached_role_policies:
            if policy['PolicyName'] not in policies:
                to_remove.append(policy)
        if to_remove:
            stderr('\nremove extra policies:')
            for policy in to_remove:
                if preview:
                    stderr(' preview:', policy['PolicyName'])
                else:
                    stderr('', policy['PolicyName'])
                    client('iam').detach_role_policy(RoleName=name, PolicyArn=policy["PolicyArn"])

def rm_extra_allows(name, allows, preview):
    to_remove = []
    try:
        role_policies = [policy for page in client('iam').get_paginator('list_role_policies').paginate(RoleName=name) for policy in page['PolicyNames']]
    except client('iam').exceptions.NoSuchEntityException:
        pass
    else:
        for policy in role_policies:
            if policy not in [_policy_name(x) for x in allows]:
                to_remove.append(policy)
        if to_remove:
            stderr('\nremove extra allows:')
            for policy in to_remove:
                if preview:
                    stderr(' preview:', policy)
                else:
                    stderr('', policy)
                    client('iam').delete_role_policy(RoleName=name, PolicyName=policy)

def rm_role(name):
    try:
        client('iam').get_role(RoleName=name)
    except client('iam').exceptions.NoSuchEntityException:
        return
    else:
        stderr(name)
        policies = [policy for page in client('iam').get_paginator('list_attached_role_policies').paginate(RoleName=name) for policy in page['AttachedPolicies']]
        for policy in policies:
            client('iam').detach_role_policy(RoleName=name, PolicyArn=policy["PolicyArn"])
            stderr(' detached policy:', policy["PolicyName"])
        role_policies = [policy for page in client('iam').get_paginator('list_role_policies').paginate(RoleName=name) for policy in page['PolicyNames']]
        for policy in role_policies:
            client('iam').delete_role_policy(RoleName=name, PolicyName=policy)
            stderr(' deleted policy:', policy)
        profiles = [profile['InstanceProfileName']
                    for page in client('iam').get_paginator('list_instance_profiles_for_role').paginate(RoleName=name)
                    for profile in page['InstanceProfiles']]
        for profile in profiles:
            client('iam').remove_role_from_instance_profile(InstanceProfileName=profile, RoleName=name)
            stderr(' detached from profile:', profile)
        client('iam').delete_role(RoleName=name)
        stderr(' deleted role:', name)

def rm_instance_profile(name):
    try:
        client('iam').get_instance_profile(InstanceProfileName=name)
    except client('iam').exceptions.NoSuchEntityException:
        return
    else:
        for role in client('iam').get_instance_profile(InstanceProfileName=name)['InstanceProfile']['Roles']:
            rm_role(role['RoleName'])
        client('iam').delete_instance_profile(InstanceProfileName=name)
        stderr(' deleted instance profile:', name)
