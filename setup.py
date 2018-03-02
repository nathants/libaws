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
                      'argh==0.26.2',
                      'py-util',
                      'py-shell',
                      'py-pool'],
    dependency_links=['https://github.com/nathants/py-util/tarball/81d2569f92c0ff9dbeded38c710ed63682dd48af#egg=py-util-0',
                      'https://github.com/nathants/py-pool/tarball/88bb744cbc6dc2a37499a0308372c6507129186a#egg=py-pool-0',
                      'https://github.com/nathants/py-shell/tarball/a91acd6224ff0d80b846b302641e4c0770a23dbc#egg=py-shell-0'],
    scripts = [x for x in os.listdir(parent) if x.startswith('aws-')],
    description='composable, succinct aws scripts',
)
