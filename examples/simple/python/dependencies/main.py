#!/usr/bin/env python3

import os

def main(event, context):
    import foo
    print(foo.bar(os.environ['uid']))
