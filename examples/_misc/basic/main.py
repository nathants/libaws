#!/usr/bin/env python3

import requests
import util.colors
import os

def main(event, context):
    print('log some stuff about requests:', requests.get)
    print(util.colors.green('green means go'))
    print(os.environ['uid'])
    return ' '.join(f'{k}=>{v}' for k, v in event.items())
