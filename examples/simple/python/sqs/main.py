#!/usr/bin/env python3

def main(event, context):
    for record in event['Records']:
        print('thanks for:', record['body'])
