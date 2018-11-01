import setuptools
import os

parent = os.path.dirname(os.path.abspath(__file__))

setuptools.setup(
    version="0.0.1",
    license='mit',
    name='cli-aws',
    author='nathan todd-stone',
    author_email='me@nathants.com',
    url='http://github.com/nathants/aws-util',
    py_modules=['cli_aws'],
    python_requires='>=3.6',
    install_requires=['requests >2, <3',
                      'boto3 >1, <2',
                      'awscli >1, <2',
                      'argh >0.26, <0.27',
                      'py-util',
                      'py-shell',
                      'py-pool'],
    dependency_links=['https://github.com/nathants/py-util/tarball/0e2f7c7637bb2907a817b343712289d64119377b#egg=py-util-0.0.1',
                      'https://github.com/nathants/py-pool/tarball/784c70058fe7bb835fe05e38c49b6632b09f242d#egg=py-pool-0.0.1',
                      'https://github.com/nathants/py-shell/tarball/19d7d2a873c4cfff6a8dc0b202a4f68678e95a9a#egg=py-shell-0.0.1'],
    scripts = [os.path.join(service, script)
               for service in os.listdir(parent)
               if service.startswith('aws-')
               and os.path.isdir(service)
               for script in os.listdir(os.path.join(parent, service))
               for path in [os.path.join(service, script)]
               if os.path.isfile(path)],
    description='composable, succinct aws scripts',
)
