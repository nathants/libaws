#!/usr/bin/env python3.6
#
# policy: CloudWatchLogsFullAccess
# trigger: cloudwatch rate(1 minute)

import pprint

def main(event, context):
    """

    """
    print('scheduled trigger:', pprint.pformat(event))
