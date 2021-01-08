from shell import run
import shell as sh
import pytest
import uuid
import contextlib

@contextlib.contextmanager
def ec2_name():
    with sh.set_stream():
        name = str(uuid.uuid4())
        try:
            yield name
        except:
            raise
        finally:
            try:
                ids = run('aws-ec2-id', name).splitlines()
            except:
                pass
            else:
                print('cleaning up left over ec2 ids:', *ids)
                run('aws-ec2-rm -y', *ids)

def test_new_instance():
    args = '--ami bionic --type t3.nano'
    with ec2_name() as name:
        assert 1 == run('aws-ec2-ls --state running', name, warn=True)['exitcode'] # nothing exists
        instance_id = run('aws-ec2-new', args, name)                               # make 1 instance and keep the id
        instances = run('aws-ec2-ls', name).splitlines()                           # list it
        assert instances == run('aws-ec2-ls', instance_id).splitlines()            # should list the same by either name or id
        assert 1 == len(instances)                                                 # should be 1 instance
        run('aws-ec2-rm -y', instance_id)                                          # terminate it
        assert 1 == run('aws-ec2-ls --state running', name, warn=True)['exitcode'] # nothing exists

def test_new_instances():
    args = '--ami bionic --type t3.nano --num 3'
    with ec2_name() as name:
        assert 1 == run('aws-ec2-ls --state running', name, warn=True)['exitcode']    # nothing exists
        instance_ids = run('aws-ec2-new', args, name).splitlines()                    # make 3 instances and keep the ids
        instances = run('aws-ec2-ls', name).splitlines()                              # list them
        assert instances == run('aws-ec2-ls', *instance_ids).splitlines()             # should list the same by either name or ids
        assert 3 == len(instances)                                                    # should be 3 instance
        run('aws-ec2-rm -y', *instance_ids)                                           # terminate them
        assert 1 == run('aws-ec2-ls --state running', name, warn=True)['exitcode']    # nothing exists

def test_new_spot_instances():
    args = '--ami bionic --type t3.nano --spot --num 3'
    with ec2_name() as name:
        assert 1 == run('aws-ec2-ls --state running', name, warn=True)['exitcode']    # nothing exists
        instance_ids = run('aws-ec2-new', args, name).splitlines()                    # make 3 instances and keep the ids
        instances = run('aws-ec2-ls', name).splitlines()                              # list them
        assert instances == run('aws-ec2-ls', *instance_ids).splitlines()             # should list the same by either name or ids
        assert 3 == len(instances)                                                    # should be 3 instance
        run('aws-ec2-rm -y', *instance_ids)                                           # terminate them
        assert 1 == run('aws-ec2-ls --state running', name, warn=True)['exitcode']    # nothing exists

if __name__ == '__main__':
    pytest.main(['-s', '-x', '--tb', 'native', __file__])
