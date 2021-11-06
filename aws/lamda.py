from aws import stderr, retry, client
import glob
import json
import aws
import sys
import re
import os
import shell as sh
import aws.api
import aws.s3
import aws.dynamodb
from aws import client

stage_name = 'main'

def set_concurrency(name, concurrency, preview):
    if concurrency:
        if preview:
            stderr('\npreview: concurrency:', concurrency)
        else:
            client('lambda').put_function_concurrency(FunctionName=name, ReservedConcurrentExecutions=concurrency)
            stderr('\nconcurrency:', concurrency)
    else:
        if preview:
            stderr('\npreview: concurrency:', concurrency)
        else:
            client('lambda').delete_function_concurrency(FunctionName=name)
            stderr('\nconcurrency:', concurrency)

def name(path):
    if not os.path.isfile(path):
        return path
    name = os.path.basename(path).replace(' ', '-').replace('_', '-').split('.py')[0]
    assert '.' not in name, '"." should not be in your filepath except for a file extension'
    return name

def arn(name):
    not_found = client('lambda').exceptions.ResourceNotFoundException
    return retry(client('lambda').get_function, not_found)(FunctionName=name)['Configuration']['FunctionArn']

def filter_metadata(lines):
    for line in lines:
        line = line.strip()
        if line.startswith('#') and ':' in line:
            yield line.strip('# ')
        if line.split() and line.split()[0] in {'import', 'def'}:
            break

def parse_metadata(token, lines, silent=False):
    vals = [(line, line.split(token, 1)[-1].split('#')[0].strip())
            for line in filter_metadata(lines)
            if line.startswith(token)]
    new_vals = []
    for line, val in vals:
        if '$' in val:
            try:
                val = ''.join([os.environ[part[2:-1]] if part.startswith('$') else part for part in re.split('(\$\{[^\}]+})', val)])
            except KeyError:
                stderr(f'fatal: missing environment: {line}')
                sys.exit(1)
        new_vals.append(val)
    vals = new_vals
    if vals and not silent:
        stderr(token)
        for val in vals:
            stderr('', val)
        stderr()
    return vals

def metadata(lines, silent=False):
    meta = {
        's3':           parse_metadata('s3:', lines, silent),
        'dynamodb':     parse_metadata('dynamodb:', lines, silent),
        'sqs':          parse_metadata('sqs:', lines, silent),
        'policy':       parse_metadata('policy:', lines, silent),
        'allow':        parse_metadata('allow:', lines, silent),
        'include':      parse_metadata('include:', lines, silent),
        'trigger':      parse_metadata('trigger:', lines, silent),
        'require':      [os.path.expanduser(x) for x in parse_metadata('require:', lines, silent)],
        'attr':         parse_metadata('attr:', lines, silent),
    }
    for line in meta['attr']:
        key = line.split()[0]
        assert key in {'concurrency', 'memory', 'timeout'}, f'unknown attr: "attr: {key}"'
    for trigger in meta['trigger']:
        assert trigger.split()[0] in {'sqs', 's3', 'dynamodb', 'api', 'cloudwatch'}, f'unknown trigger: "{trigger}"'
    for line in filter_metadata(lines):
        token = line.split(':')[0]
        assert token in meta, f'unknown configuration comment: "{token}: ..."'
    for k, v in list(meta.items()):
        if not len(v):
            meta.pop(k)
    return meta

def ensure_trigger_s3(name, arn_lambda, metadata, preview):
    events = ['s3:ObjectCreated:*', 's3:ObjectRemoved:*']
    triggers = []
    for trigger in metadata['trigger']:
        if trigger.split()[0] == 's3':
            kind, bucket, *_ = trigger.split()
            triggers.append(bucket)
    if triggers:
        stderr('\nensure triggers s3:')
        for bucket in triggers:
            if preview:
                stderr(' preview:', bucket)
            else:
                ensure_permission(name, 's3.amazonaws.com', f'arn:aws:s3:::{bucket}')
                confs = aws.resource('s3').BucketNotification(bucket).lambda_function_configurations or []
                for conf in confs:
                    if conf['LambdaFunctionArn'] == arn_lambda and conf['Events'] == events:
                        stderr('', bucket)
                        break
                else:
                    confs.append({'LambdaFunctionArn': arn_lambda, 'Events': events})
                    stderr('', bucket)
                aws.resource('s3').BucketNotification(bucket).put(NotificationConfiguration={'LambdaFunctionConfigurations': confs})

def ensure_trigger_api(name, arn_lambda, metadata, preview):
    for trigger in metadata['trigger']:
        if trigger.split()[0] == 'api':
            if preview:
                stderr('\npreview: ensure triggers api')
            else:
                stderr('\nensure triggers api')
                try:
                    rest_api_id = aws.api.api_id(name)
                except AssertionError:
                    rest_api_id = client('apigateway').create_rest_api(name=name,
                                                                       binaryMediaTypes=['*/*'],
                                                                       endpointConfiguration={'types': ['REGIONAL']})['id']
                parent_id = aws.api.resource_id(rest_api_id, '/')
                resource_id = aws.api.resource_id(rest_api_id, '/{proxy+}')
                if not resource_id:
                    resource_id = client('apigateway').create_resource(restApiId=rest_api_id, parentId=parent_id, pathPart='{proxy+}')['id']
                api = client('lambda').meta.service_model.api_version
                uri = f"arn:aws:apigateway:{aws.region()}:lambda:path/{api}/functions/"
                uri += f'arn:aws:lambda:{aws.region()}:{aws.account()}:function:{name}/invocations'
                for id in [parent_id, resource_id]:
                    try:
                        client('apigateway').put_method(restApiId=rest_api_id, resourceId=id, httpMethod='ANY', authorizationType='NONE')
                    except client('apigateway').exceptions.ConflictException:
                        pass
                    else:
                        client('apigateway').put_integration(restApiId=rest_api_id, resourceId=id, httpMethod='ANY', type="AWS_PROXY", integrationHttpMethod='POST', uri=uri)
                client('apigateway').create_deployment(restApiId=rest_api_id, stageName=stage_name)
                arn = f"arn:aws:execute-api:{aws.region()}:{aws.account()}:{rest_api_id}/*/*/*"
                ensure_permission(name, 'apigateway.amazonaws.com', arn)
            break

def ensure_trigger_cloudwatch(name, arn_lambda, metadata, preview):
    triggers = []
    for trigger in metadata['trigger']:
        if trigger.split()[0] == 'cloudwatch':
            kind, schedule = trigger.split(None, 1)
            triggers.append(schedule)
    if triggers:
        stderr('\nensure triggers cloudwatch:')
        assert len(triggers) == 1, f'only 1 cloudwatch schedule is currently supported: {triggers}'
        for schedule in triggers:
            if preview:
                stderr(' preview:', schedule)
            else:
                arn_rule = client('events').put_rule(Name=name, ScheduleExpression=schedule)['RuleArn']
                ensure_permission(name, 'events.amazonaws.com', arn_rule)
                targets = retry(client('events').list_targets_by_rule)(Rule=name)['Targets']
                assert all(t['Arn'] == arn_lambda for t in targets), f'there are unknown targets in cloudwatch rule: {name}'
                if len(targets) == 0:
                    stderr('', schedule)
                    client('events').put_targets(Rule=name, Targets=[{'Id': '1', 'Arn': arn_lambda}])
                elif len(targets) == 1:
                    assert targets[0]['Arn'] == arn_lambda, f'cloudwatch target mismatch: {arn_lambda} {targets[0]}'
                    stderr('', schedule)
                elif len(targets) > 1:
                    stderr(' removing:', schedule)
                    targets = sorted(targets, key=lambda x: x['Id'])
                    client('events').remove_targets(Rule=name, Ids=[t['Id'] for t in targets[1:]])
                def ensure_only_one_target():
                    targets = client('events').list_targets_by_rule(Rule=name)['Targets']
                    assert len(targets) == 1, f'more than one target found for cloudwatch rule: {name} {schedule} {targets}'
                retry(ensure_only_one_target)()

trigger_dynamodb_attr_shortcuts = {
    'start': 'StartingPosition',
    'batch': 'BatchSize',
    'parallel': 'ParallelizationFactor',
    'retry': 'MaximumRetryAttempts',
}

def ensure_trigger_dynamodb(name, arn_lambda, metadata, preview):
    triggers = []
    for trigger in metadata['trigger']:
        if trigger.split()[0] == 'dynamodb':
            kind, table_name, *attrs = trigger.split()
            triggers.append([table_name, attrs])
    if triggers:
        stderr('\nensure triggers dynamodb:')
        for table_name, attrs in triggers:
            ensure_attrs = {k: int(v) if v.isdigit() else v for a in attrs for k, v in [a.split('=')]}
            for k, v in trigger_dynamodb_attr_shortcuts.items():
                if k in ensure_attrs:
                    ensure_attrs[v] = ensure_attrs.pop(k)
            if 'StartingPosition' in ensure_attrs:
                ensure_attrs['StartingPosition'] = ensure_attrs['StartingPosition'].upper()
            if preview:
                stderr(' preview:', table_name)
            else:
                stream_arn = aws.dynamodb.stream_arn(table_name)
                conflict = client('lambda').exceptions.ResourceConflictException
                try:
                    retry(client('lambda').create_event_source_mapping, conflict)(EventSourceArn=stream_arn, FunctionName=name, Enabled=True, **ensure_attrs)
                    stderr('', table_name)
                except conflict as e:
                    *_, kind, uuid = e.args[0].split()
                    resp = client('lambda').get_event_source_mapping(UUID=uuid)
                    for k, v in ensure_attrs.items():
                        if k != 'StartingPosition':
                            assert resp[k] == v, [resp[k], v]
                    stderr('', table_name)

trigger_sqs_attr_shortcuts = {
    'start': 'StartingPosition',
    'batch': 'BatchSize',
    'parallel': 'ParallelizationFactor',
    'retry': 'MaximumRetryAttempts',
}

def ensure_trigger_sqs(name, arn_lambda, metadata, preview):
    triggers = []
    for trigger in metadata['trigger']:
        if trigger.split()[0] == 'sqs':
            kind, queue_name, *attrs = trigger.split()
            triggers.append([queue_name, attrs])
    if triggers:
        stderr('\nensure triggers sqs:')
        for queue_name, attrs in triggers:
            ensure_attrs = {k: int(v) if v.isdigit() else v for a in attrs for k, v in [a.split('=')]}
            for k, v in trigger_sqs_attr_shortcuts.items():
                if k in ensure_attrs:
                    ensure_attrs[v] = ensure_attrs.pop(k)
            if 'StartingPosition' in ensure_attrs:
                ensure_attrs['StartingPosition'] = ensure_attrs['StartingPosition'].upper()
            if preview:
                stderr(' preview:', queue_name)
            else:
                stream_arn = aws.dynamodb.stream_arn(queue_name)
                try:
                    client('lambda').create_event_source_mapping(EventSourceArn=stream_arn, FunctionName=name, Enabled=True, **ensure_attrs)
                    stderr('', queue_name)
                except client('lambda').exceptions.ResourceConflictException as e:
                    *_, kind, uuid = e.args[0].split()
                    resp = client('lambda').get_event_source_mapping(UUID=uuid)
                    for k, v in ensure_attrs.items():
                        if k != 'StartingPosition':
                            assert resp[k] == v, [resp[k], v]
                    stderr('', queue_name)

def ensure_permission(name, principal, arn):
    not_found = client('lambda').exceptions.ResourceNotFoundException
    try:
        res = json.loads(retry(client('lambda').get_policy, not_found)(FunctionName=name)['Policy'])
    except not_found:
        statements = []
    else:
        statements = [x['Sid'] for x in res['Statement']]
    id = principal.replace('.', '-') + '__' + arn.split(':')[-1].replace('-', '_').replace('/', '__').replace('*', 'ALL')
    if id not in statements:
        client('lambda').add_permission(FunctionName=name, StatementId=id, Action='lambda:InvokeFunction', Principal=principal, SourceArn=arn)

def parse_file(path, silent=False):
    if not os.path.isfile(path):
        stderr('no such file:', path)
        sys.exit(1)
    if not path.endswith('.py') or len(path.split('.py')) > 2:
        stderr('usage: python deploy.py some_lambda_file.py')
        sys.exit(1)
    with open(path) as f:
        meta = metadata(f.read().splitlines(), silent=silent)
    return path, meta

def zip_file(path):
    return f'/tmp/{name(path)}/lambda.zip'

def create_zip(path, requires, preview):
    stderr('\ncreate zip:')
    _zip_file = zip_file(path)
    if not preview:
        tempdir = os.path.dirname(_zip_file)
        sh.run('rm -rf', tempdir)
        sh.run('mkdir -p', tempdir)
        sh.run(f'virtualenv --python python3 {tempdir}/env')
    if requires:
        for require in requires:
            if preview:
                stderr(' preview: require:', require)
            else:
                stderr(' require:', require)
        if not preview:
            with sh.cd(os.path.dirname(path)):
                sh.run(f'{tempdir}/env/bin/pip install', *[f'"{r}"' for r in requires])
    if not preview:
        [site_packages] = glob.glob(f'{tempdir}/env/lib/python3*/site-packages')
        with sh.cd(site_packages):
            sh.run(f'cp {path} .')
            sh.run('rm -rf wheel pip setuptools pkg_resources easy_install.py')
            sh.run("ls | grep -E 'info$' | grep -v ' ' | xargs rm -rf")
            libs = sh.run('ls').splitlines()
            for binpath in sh.run(f"find {tempdir}/env/bin/ -type f,l").splitlines():
                name = os.path.basename(binpath)
                if name not in libs:
                    with open(binpath, 'rb') as f:
                        lines = f.read().splitlines()
                    if lines and lines[0].startswith(b"#!") and b'python' in lines[0]:
                        with open(name, 'wb') as f:
                            f.write(b'#!/var/lang/bin/python\n')
                            f.write(b'\n'.join(lines[1:]))
                        sh.run('chmod +x', name)
            sh.run(f'zip -r {_zip_file} .')

def zip_bytes(path):
    _zip_file = zip_file(path)
    with open(_zip_file, 'rb') as f:
        return f.read()

def include_in_zip(path, includes, preview):
    _zip_file = zip_file(path)
    with sh.cd(os.path.dirname(os.path.abspath(path))):
        for include in includes:
            if '*' in include:
                for path in glob.glob(include):
                    if preview:
                        stderr(' preview: include:', path)
                    else:
                        stderr(' include:', path)
                        sh.run(f'zip {_zip_file} {path}')
            else:
                if preview:
                    stderr(' preview: include:', include)
                else:
                    stderr(' include:', include)
                    sh.run(f'zip {_zip_file} {include}')

def update_zip(path):
    stderr('\nupdate zip:')
    _zip_file = zip_file(path)
    tempdir = os.path.dirname(_zip_file)
    [site_packages] = glob.glob(f'{tempdir}/env/lib/python3*/site-packages')
    with sh.cd(site_packages):
        sh.run(f'cp {path} .')
        sh.run(f'zip {_zip_file} {os.path.basename(path)}')
    stderr('', _zip_file)

def ensure_infra_log_group(name, preview):
    name = f'/aws/lambda/{name}'
    stderr('ensure infra logs:')
    try:
        if preview:
            stderr(' preview:', name)
        else:
            client('logs').create_log_group(logGroupName=name)
            stderr('', name)
    except client('logs').exceptions.ResourceAlreadyExistsException:
        stderr('', name)

def ensure_infra_s3(buckets, preview):
    if buckets:
        stderr('\nensure infra s3:')
        for bucket in buckets:
            name, *args = bucket.split()
            assert not args, 'no params to s3'
            aws.s3.ensure_bucket(name, print_fn=lambda *a: stderr('', *a), preview=preview)

def ensure_infra_dynamodb(dbs, preview):
    if dbs:
        stderr('\nensure infra dynamodb:')
        for i, db in enumerate(dbs):
            name, *args = db.split()
            aws.dynamodb.ensure_table(name, *args, yes=True, print_fn=lambda *a: stderr('', *a), preview=preview)

def ensure_infra_sqs(sqss, preview):
    assert False, 'use dotted dict and move to aws'
    not_found = client('sqs').exceptions.QueueDoesNotExist
    if sqss:
        stderr('\nensure infra sqs:')
        for sqs in sqss:
            name, *attrs = sqs.split()
            if preview:
                stderr(' preview:', name)
            else:
                attrs = {k: int(v) if v.isdigit() else v for attr in attrs for k, v in [attr.split('=')]}
                try:
                    queue_url = client('sqs').get_queue_url(QueueName=sqs)
                except not_found:
                    client('sqs').create_queue(QueueName=sqs, Attributes=attrs)
                    stderr('', name)
                else:
                    queue_attrs = client('sqs').get_queue_attributes(QueueUrl=queue_url)['Attributes']
                    for k, v in attrs.items():
                        assert queue_attrs[k] == v, f'sqs attr mismatch {k} {v} != {queue_attrs[k]}'
                    stderr('', name)
