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
    install_requires=['boto3 >1, <2',
                      'awscli >1, <2',
                      'argh >0.26, <0.27',
                      'py-util',
                      'py-shell',
                      'py-pool'],
    dependency_links=['https://github.com/nathants/py-util/tarball/4d1fe20ecfc0b6982933a8c9b622b1b86da2be5e#egg=py-util-0.0.1',
                      'https://github.com/nathants/py-pool/tarball/f1e9aee71bc7d8302f0df8d9111e49e008a16351#egg=py-pool-0.0.1',
                      'https://github.com/nathants/py-shell/tarball/acffc13e959d63ba9501f07b32c2c61271415767#egg=py-shell-0.0.1'],
    scripts = [os.path.join(service, script)
               for service in os.listdir(parent)
               if service.startswith('aws-')
               and os.path.isdir(service)
               for script in os.listdir(os.path.join(parent, service))
               for path in [os.path.join(service, script)]
               if os.path.isfile(path)],
    description='composable, succinct aws scripts',
)
