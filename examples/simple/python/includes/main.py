#!/usr/bin/env python3

def main(event, context):
    with open('include_me.txt') as f:
        return f.read().strip()
