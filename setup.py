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
    python_requires='>=3.7',
    install_requires=['requests >2, <3',
                      'boto3 >1, <2',
                      'awscli >1, <2',
                      'argh >0.26, <0.27'],
    scripts = [os.path.join(service, script)
               for service in os.listdir(parent)
               if service.startswith('aws-')
               and os.path.isdir(service)
               for script in os.listdir(os.path.join(parent, service))
               for path in [os.path.join(service, script)]
               if os.path.isfile(path)],
    description='composable, succinct aws scripts',
)
