from aws import client, stderr, resource
import aws

def rm_bucket(name, print_fn=stderr):
    try:
        for page in client('s3').get_paginator('list_objects_v2').paginate(Bucket=name):
            keys = [key['Key'] for key in page.get('Contents', [])]
            if keys:
                client('s3').delete_objects(Bucket=name, Delete={'Objects': [{'Key': key} for key in keys]})
                for key in keys:
                    print_fn(f'deleted object: s3://{name}/{key}')
        for page in client('s3').get_paginator('list_object_versions').paginate(Bucket=name):
            keys = page.get('Versions', [])
            if keys:
                client('s3').delete_objects(Bucket=name, Delete={'Objects': [{'Key': key['Key'], 'VersionId': key['VersionId']} for key in keys]})
                for key in keys:
                    print_fn(f'deleted version: s3://{name}/{key["Key"]} {key["VersionId"]}')
            keys = page.get('DeleteMarkers', [])
            if keys:
                client('s3').delete_objects(Bucket=name, Delete={'Objects': [{'Key': key['Key'], 'VersionId': key['VersionId']} for key in keys]})
                for key in keys:
                    print_fn(f'deleted version: s3://{name}/{key["Key"]} {key["VersionId"]}')

        client('s3').delete_bucket(Bucket=name)
        print_fn(f'deleted bucket: s3://{name}')
    except client('s3').exceptions.NoSuchBucket:
        pass

def ensure_bucket(name, acl='private', versioning=False, noencrypt=False, print_fn=stderr, preview=False):
    if preview:
        print_fn(' preview:', name)
    else:

        try:
            client('s3').create_bucket(
                ACL=acl,
                Bucket=name,
                CreateBucketConfiguration={'LocationConstraint': aws.region()},
            )
        except client('s3').exceptions.BucketAlreadyOwnedByYou:
            print_fn('', name)
        else:
            print_fn('', name)

        if acl == 'private':
            client('s3control').put_public_access_block(
                PublicAccessBlockConfiguration={
                    'BlockPublicAcls': True,
                    'IgnorePublicAcls': True,
                    'BlockPublicPolicy': True,
                    'RestrictPublicBuckets': True
                },
                AccountId=aws.account(),
            )

        if versioning:
            resource('s3').BucketVersioning(name).enable()
        else:
            resource('s3').BucketVersioning(name).suspend()

        if noencrypt:
            client('s3').delete_bucket_encryption(Bucket=name)
        else:
            client('s3').put_bucket_encryption(
                Bucket=name,
                ServerSideEncryptionConfiguration={'Rules': [{'ApplyServerSideEncryptionByDefault': {'SSEAlgorithm': 'AES256'}}]},
            )
