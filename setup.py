import setuptools
import shutil
import sys
import os
import subprocess

setuptools.setup(
    version="0.0.1",
    license='mit',
    name='cli-aws',
    author='nathan todd-stone',
    author_email='me@nathants.com',
    url='http://github.com/nathants/cli-aws',
    packages=['aws'],
    python_requires='>=3.6',
    install_requires=['requests >2, <3',
                      'boto3 >1, <2',
                      'awscli >1, <2',
                      'argh >0.26, <0.27'],
    description='composable, succinct aws scripts',
)

parent = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'bin')

if 'develop' not in sys.argv:
    try:
        subprocess.check_call([sys.executable, '-m', 'pip', 'install', '-r', 'requirements.txt'])
    except:
        print('fatal: failure: pip install -r requirements.txt ')
        sys.exit(1)

scripts = [path
           for service in os.listdir(parent)
           if service.startswith('aws')
           for script in os.listdir(os.path.join(parent, service))
           for path in [os.path.abspath(os.path.join(parent, service, script))]
           if os.path.isfile(path)]

dst_path = os.path.dirname(os.path.abspath(sys.executable))
for src in scripts:
    name = os.path.basename(src)
    dst = os.path.join(dst_path, name)
    try:
        os.remove(dst)
    except FileNotFoundError:
        pass
    if 'develop' in sys.argv:
        os.symlink(src, dst)
        os.chmod(dst, 0o775)
        print('link:', dst, '=>', src, file=sys.stderr)
    else:
        shutil.copyfile(src, dst)
        os.chmod(dst, 0o775)
        print('copy:', src, '=>', dst, file=sys.stderr)
