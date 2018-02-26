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
    scripts = [x for x in os.listdir(parent) if x.startswith('aws-')],
    description='composable, succinct aws scripts',
)
