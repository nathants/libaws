import setuptools
import os

parent = os.path.dirname(os.path.abspath(__file__))

setuptools.setup(
    version="0.0.1",
    license='mit',
    name='aws-util',
    author='nathan todd-stone',
    author_email='me@nathants.com',
    url='http://github.com/nathants/aws-util',
    py_modules=['aws_util'],
    install_requires=['boto3 >1, <2',
                      'awscli >1, <2',
                      'argh >0.26, <0.27',
                      'py-util',
                      'py-shell',
                      'py-pool'],
    dependency_links=['https://github.com/nathants/py-util/tarball/81d2569f92c0ff9dbeded38c710ed63682dd48af#egg=py-util-0',
                      'https://github.com/nathants/py-pool/tarball/88bb744cbc6dc2a37499a0308372c6507129186a#egg=py-pool-0',
                      'https://github.com/nathants/py-shell/tarball/ec627065e019e4189c4b49639dc4508336196cfb#egg=py-shell-0'],
    scripts = [os.path.join(service, script)
               for service in os.listdir(parent)
               if service.startswith('aws-')
               and os.path.isdir(service)
               for script in os.listdir(os.path.join(parent, service))],
    description='composable, succinct aws scripts',
)
