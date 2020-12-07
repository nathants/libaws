from aws import client, stderr
import aws
import util.iter


def rm_bucket(name, print_fn=stderr):
    try:
        keys = (
            key['Key']
            for page in client('s3').get_paginator('list_objects_v2').paginate(Bucket=name)
            for key in page.get('Contents', [])
        )
        for chunk in util.iter.ichunk(keys, 1000):
            chunk = list(chunk)
            client('s3').delete_objects(Bucket=name, Delete={'Objects': [{'Key': key} for key in chunk]})
            for key in chunk:
                print_fn(f'deleted object: s3://{name}/{key}')
        client('s3').delete_bucket(Bucket=name)
        print_fn(f'deleted bucket: s3://{name}')
    except client('s3').exceptions.NoSuchBucket:
        pass

def ensure_bucket(name, acl='private', print_fn=stderr, preview=False):
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
