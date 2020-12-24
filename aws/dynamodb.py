import aws
import shell as sh
from util import dicts
from aws import retry, stderr, client

def unverbose(v):
    if isinstance(v, dict):
        if len(v) == 1 and list(v)[0] in ['S', 'N', 'B', 'L', 'M', 'BOOL']:
            v = list(v.values())[0]
        else:
            for k2, v2 in list(v.items()):
                if v2 == {'NULL': True}:
                    v.pop(k2)
    return v

def arn(name):
    return f'arn:aws:dynamodb:{aws.region()}:{aws.account()}:table/{name}'

def stream_arn(name):
    not_found = client('dynamodb').exceptions.ResourceNotFoundException
    return retry(client('dynamodb').describe_table, not_found)(TableName=name)['Table']['LatestStreamArn']

table_attr_shortcuts = {
    'read': 'ProvisionedThroughput.ReadCapacityUnits',
    'write': 'ProvisionedThroughput.WriteCapacityUnits',
    'stream': 'StreamSpecification.StreamViewType',
}

def ensure_table(name, *attrs, preview=False, yes=False, print_fn=stderr):
    # grab some exception shortcuts
    table_exists = client('dynamodb').exceptions.ResourceInUseException
    not_found = client('dynamodb').exceptions.ResourceNotFoundException
    client_error = client('dynamodb').exceptions.ClientError
    # start ensure_attrs with columns
    columns = [attr for attr in attrs if '=' not in attr]
    ensure_attrs = dicts.to_dotted({
        'AttributeDefinitions': [{'AttributeName': attr_name, 'AttributeType': attr_type.upper()}
                                 for column in columns
                                 for attr_name, attr_type, _ in [column.split(':')]],
        'KeySchema': [{'AttributeName': attr_name, 'KeyType': key_type.upper()}
                      for column in columns
                      for attr_name, _, key_type in [column.split(':')]]
    })
    # update ensure_attrs with the rest of the passed attributes
    ensure_attrs.update({k: int(v) if v.isdigit() else v
                         for attr in attrs
                         if '=' in attr
                         for k, v in [attr.split('=')]})
    # resolve any attribute shortcuts
    for k, v in table_attr_shortcuts.items():
        if k in ensure_attrs:
            ensure_attrs[v] = ensure_attrs.pop(k)
    # allow lower case for stream view type
    if 'StreamSpecification.StreamViewType' in ensure_attrs:
        ensure_attrs['StreamSpecification.StreamEnabled'] = True
        ensure_attrs['StreamSpecification.StreamViewType'] = ensure_attrs['StreamSpecification.StreamViewType'].upper()
    # check provisioning and set billing type
    read = ensure_attrs.get('ProvisionedThroughput.ReadCapacityUnits')
    write = ensure_attrs.get('ProvisionedThroughput.WriteCapacityUnits')
    assert (not read and not write) or (read and write), 'both read and write must be provisioned, or neither for on-demand'
    ensure_attrs['BillingMode'] = 'PROVISIONED' if read else 'PAY_PER_REQUEST'
    # print and prompt
    print_fn()
    print_fn('TableName:', name)
    for k, v in ensure_attrs.items():
        print_fn(f' {k}: {v}')
    print_fn()
    # fetch existing table attrs
    try:
        existing_attrs = dicts.to_dotted(client('dynamodb').describe_table(TableName=name)['Table'])
    # create table
    except not_found:
        # create
        if preview:
            print_fn(' preview: created:', name)
        else:
            if not yes:
                print_fn('\nproceed? y/n ')
                assert sh.getch() == 'y'
            retry(client('dynamodb').create_table, table_exists, client_error)(TableName=name, **dicts.from_dotted(ensure_attrs))
            print_fn(' created:', name)
    # check and maybe update existing table
    else:
        if preview:
            print_fn(' preview: exists:', name)
        else:
            print_fn(' exists:', name)
        # join tags into existing attributes
        existing_attrs.update(dicts.to_dotted({'Tags': [
            tag
            for page in retry(client('dynamodb').get_paginator('list_tags_of_resource').paginate)(ResourceArn=arn(name))
            for tag in page['Tags']]
        }))
        # remap existing attributes to the same schema as table attributes
        existing_attrs = {k.replace('BillingModeSummary.', ''): v for k, v in existing_attrs.items()}
        # check every attribute
        needs_update = False
        for k, v in ensure_attrs.items():
            if v != existing_attrs.get(k):
                needs_update = True
                if preview:
                    print_fn(f' preview: {k}: {existing_attrs.get(k)} -> {v}')
                else:
                    print_fn(f' {k}: {existing_attrs.get(k)} -> {v}')
                assert k.split('.')[0] != 'KeySchema', 'KeySchema cannot be updated on existing tables'
        # collect tags to remove
        tags_to_remove = []
        for tag in dicts.from_dotted(existing_attrs).get('Tags', []):
            if tag['Key'] not in [t['Key'] for t in dicts.from_dotted(ensure_attrs).get('Tags', [])]:
                tags_to_remove.append(tag['Key'])
                if preview:
                    print_fn(f' preview: untag: {tag["Key"]}')
                else:
                    print_fn(f' untag: {tag["Key"]}')
        if not preview:
            # prompt if updates
            if (needs_update or tags_to_remove) and not yes:
                print_fn('\nproceed? y/n ')
                assert sh.getch() == 'y'
            # update if needed
            if needs_update:
                # update tags
                ensure_attrs = dicts.from_dotted(ensure_attrs)
                if 'Tags' in ensure_attrs:
                    for tag in ensure_attrs['Tags']:
                        tag['Value'] = str(tag['Value'])
                    client('dynamodb').tag_resource(ResourceArn=arn(name), Tags=ensure_attrs['Tags'])
                    del ensure_attrs['Tags']
                # update table. note: KeySchema cannot be updated
                del ensure_attrs['KeySchema']
                client('dynamodb').update_table(TableName=name, **ensure_attrs)
            # untag if needed
            if tags_to_remove:
                # remove unused tags
                if tags_to_remove:
                    client('dynamodb').untag_resource(ResourceArn=arn(name), TagKeys=tags_to_remove)

def rm_table(name, print_fn=stderr):
    in_use = client('dynamodb').exceptions.ResourceInUseException
    not_found = client('dynamodb').exceptions.ResourceNotFoundException
    try:
        retry(client('dynamodb').delete_table, in_use, not_found)(TableName=name)
    except in_use as e:
        assert str(e).endswith(f'Table is being deleted: {name}'), e
    except not_found:
        pass
    else:
        print_fn('dynamodb deleted:', name)
