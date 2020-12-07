from aws import retry, stderr
import aws
import base64
import json
import sys

def encrypt(key_id, text):
    text = bytes(text, 'utf-8')
    text = aws.client('kms').encrypt(KeyId=key_id, Plaintext=text)['CiphertextBlob']
    return base64.b64encode(text).decode('utf-8')

def ensure_key_allows_role(arn_key, arn_role, preview):
    if not preview:
        resp = aws.client('kms').get_key_policy(KeyId=arn_key, PolicyName='default')
        policy = json.loads(resp['Policy'])
        # ensure that every Statement.Principal.AWS is a list, it can be either a
        # string or a list of strings.
        for statement in policy['Statement']:
            if statement.get('Principal', {}).get('AWS'):
                if isinstance(statement['Principal']['AWS'], str):
                    statement['Principal']['AWS'] = [statement['Principal']['AWS']]
        # remove invalid principals from all statements, these are caused by the
        # deletion of an iam role referenced by this policy, which transforms the
        # principal from something like "arn:..." to "AIEKFJ...".
        for statement in policy['Statement']:
            if statement.get('Principal', {}).get('AWS'):
                for arn in statement['Principal']['AWS'].copy():
                    if not arn.startswith('arn:'):
                        statement['Principal']['AWS'].remove(arn)
        # ensure that the "allow use of key" Statement contains our role's arn
        for statement in policy['Statement']:
            if statement['Sid'] == 'Allow use of the key':
                if arn_role not in statement['Principal']['AWS']:
                    statement['Principal']['AWS'].append(arn_role)
                break
        # if an "allow use of key" Statement didn't exist, create it
        else:
            policy['Statement'].append({
                "Sid": "Allow use of the key",
                "Effect": "Allow",
                "Principal": {"AWS": [arn_role]},
                "Action": ["kms:Encrypt", "kms:Decrypt", "kms:ReEncrypt*", "kms:GenerateDataKey*", "kms:DescribeKey"],
                "Resource": "*"
            })
        try:
            retry(aws.client('kms').put_key_policy, silent=True)(KeyId=arn_key, Policy=json.dumps(policy), PolicyName='default')
        except aws.client('kms').exceptions.MalformedPolicyDocumentException:
            stderr(f'fatal: failed to put to key: {arn_key}, policy:\n' + json.dumps(policy, indent=2))
            raise

def all_keys():
    return [alias for page in aws.client('kms').get_paginator('list_aliases').paginate() for alias in page['Aliases']]

def key_id(key):
    return key['AliasArn'].split(':alias/')[0] + f':key/{key["TargetKeyId"]}'

def ensure_key(name, arn_user, arn_role, preview):
    stderr('\nensure kms key:')
    if preview:
        stderr(' preview: kms:', name)
    else:
        keys = [x for x in all_keys() if x['AliasArn'].endswith(f':alias/lambda/{name}')]
        if 0 == len(keys):
            arn_root = ':'.join(arn_user.split(':')[:-1]) + ':root'
            policy = """
            {"Version": "2012-10-17",
             "Statement": [{"Sid": "Enable IAM User Permissions",
                            "Effect": "Allow",
                            "Principal": {"AWS": ["%(arn_user)s", "%(arn_root)s"]},
                            "Action": "kms:*",
                            "Resource": "*"},
                           {"Sid": "Allow use of the key",
                            "Effect": "Allow",
                            "Principal": {"AWS": ["%(arn_user)s", "%(arn_role)s", "%(arn_root)s"]},
                            "Action": ["kms:Encrypt", "kms:Decrypt", "kms:ReEncrypt*", "kms:GenerateDataKey*", "kms:DescribeKey"],
                            "Resource": "*"},
                           {"Sid": "Allow attachment of persistent resources",
                            "Effect": "Allow",
                            "Principal": {"AWS": ["%(arn_user)s", "%(arn_role)s", "%(arn_root)s"]},
                            "Action": ["kms:CreateGrant", "kms:ListGrants", "kms:RevokeGrant"],
                            "Resource": "*",
                            "Condition": {"Bool": {"kms:GrantIsForAWSResource": true}}}]}
            """ % {'arn_role': arn_role,
                   'arn_user': arn_user,
                   'arn_root': arn_root}
            _key_id = retry(aws.client('kms').create_key, silent=True)(Policy=policy, Description=name)['KeyMetadata']['KeyId']
            aws.client('kms').create_alias(AliasName=f'alias/lambda/{name}', TargetKeyId=_key_id)
            keys = [x for x in all_keys() if x['AliasArn'].endswith(f':alias/lambda/{name}')]
            assert len(keys) == 1
            stderr('', keys[0]['AliasArn'])
            return key_id(keys[0])
        elif 1 == len(keys):
            stderr('', keys[0]['AliasArn'])
            return key_id(keys[0])
        else:
            stderr('fatal: found more than 1 key for:', name, '\n' + '\n'.join(keys))
            sys.exit(1)
