#!/usr/bin/env python3
import sys
import os
import subprocess

cc = lambda *a: subprocess.check_call(' '.join(map(str, a)), shell=True, executable='/bin/bash')
co = lambda *a: subprocess.check_output(' '.join(map(str, a)), shell=True, executable='/bin/bash').decode('utf-8').strip()

os.chdir(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

try:
    dest = sys.argv[1]
except IndexError:
    dest = os.path.expanduser('~/.completions.d')

with open('completions.d/_completion.sh') as f:
    completion = f.read()

for path, dirs, files in os.walk('.'):
    for file_path in files:
        file_path = os.path.join(path, file_path)
        if '/.' not in file_path:
            try:
                with open(file_path) as f:
                    text = f.read()
                if 'def main(*selectors' in text and os.path.basename(file_path) != 'install.py':
                    name = os.path.basename(file_path)
                    dest_path = os.path.join(dest, name) + '.sh'
                    with open(dest_path, 'w') as f:
                        f.write(completion.replace('NAME', name))
                    print('installed:', dest_path)
            except UnicodeDecodeError:
                pass
