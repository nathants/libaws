import setuptools
import os

parent = os.path.dirname(os.path.abspath(__file__))

setuptools.setup(
    version="0.0.1",
    name='adep',
    py_modules=[x.split('.py')[0]
                for x in os.listdir(parent)
                if x.endswith('.py')
                and not x == 'setup.py'],
)
