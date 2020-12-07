import boto3
import shell as sh
import datetime
import contextlib
import traceback
import logging
import os
import sys
import util.iter
import util.log
import util.cached
import util.dicts
import util.retry
from util.colors import red

stderr = lambda *a: print(*a, file=sys.stderr)

def retry(*a, **kw):
    exponent = kw.pop('exponent', 1.3)
    return util.retry.retry(*a, **kw, exponent=exponent)

@util.cached.memoize
def client(name):
    return boto3.client(name)

@util.cached.memoize
def resource(name):
    return boto3.resource(name)

def now():
    return str(datetime.datetime.utcnow().isoformat()) + 'Z'

def regions():
    return [x['RegionName'] for x in client('ec2').describe_regions()['Regions']]

def zones():
    return [x['ZoneName'] for x in client('ec2').describe_availability_zones()['AvailabilityZones']]

@contextlib.contextmanager
def setup():
    util.log.setup(format='%(message)s')
    logging.getLogger('botocore').setLevel('ERROR')
    if 'region' in os.environ:
        boto3.setup_default_session(region_name=os.environ['region'])
    elif 'REGION' in os.environ:
        boto3.setup_default_session(region_name=os.environ['REGION'])
    if util.misc.override('--shell-stream'):
        sh.set['stream'] = 'yes'
    try:
        yield
    except AssertionError as e:
        print(red('error: ' + e.args[0] if e.args else traceback.format_exc().splitlines()[-2].strip()))
        sys.exit(1)
    except sh.ExitCode as e:
        for arg in e.args:
            print(arg)
        sys.exit(1)
    except:
        raise

@util.cached.func
def region():
    client('ec2') # run session setup logic
    return boto3.DEFAULT_SESSION.region_name

@util.cached.func
def account():
    return client('sts').get_caller_identity()['Account']

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
