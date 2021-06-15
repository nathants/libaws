from typing import Dict, Any
import sys
import time
import datetime
import aws
from aws import stderr

def most_recent_streams(group_name, max_age_seconds=60 * 60 * 24):
    streams = []
    for stream in aws.client('logs').describe_log_streams(logGroupName=group_name, orderBy='LastEventTime', descending=True)['logStreams']:
        if time.time() - (stream['lastEventTimestamp'] // 1000) < max_age_seconds:
            streams.append(stream['logStreamName'])
    return streams

def tail(group_name, follow=False, timestamps=False, exit_after=None):
    stderr('group:', group_name)
    if follow:
        tokens = {}
        limit = 3 # when starting to follow, dont page all history, just grab the last few entries and then start following
        while True:
            try:
                stream_names = most_recent_streams(group_name)
            except (IndexError, aws.client('logs').exceptions.ResourceNotFoundException):
                pass
            else:
                for stream_name in stream_names:
                    kw: Dict[str, Any] = {}
                    token = tokens.get(stream_name)
                    if token:
                        kw['nextToken'] = token
                    if limit != 0:
                        kw['limit'] = limit
                        limit = 0
                    resp = aws.client('logs').get_log_events(logGroupName=group_name, logStreamName=stream_name, **kw)
                    if resp['events']:
                        tokens[stream_name] = resp['nextForwardToken']
                    for log in resp['events']:
                        if log['message'].split()[0] not in ['START', 'END', 'REPORT']:
                            if timestamps:
                                print(datetime.datetime.fromtimestamp(log['timestamp'] / 1000), log['message'].replace('\t', ' ').strip(), flush=True)
                            else:
                                print(log['message'].replace('\t', ' ').strip(), flush=True)
                        if exit_after and exit_after in log['message']:
                            sys.exit(0)
            time.sleep(1)
    else:
        try:
            stream_names = most_recent_streams(group_name)
        except IndexError:
            stderr('no logs available')
            sys.exit(1)
        else:
            for stream_name in stream_names:
                stderr('group:', group_name, 'stream:', stream_name)
                logs = aws.client('logs').get_log_events(logGroupName=group_name, logStreamName=stream_name)['events']
                for log in logs:
                    if log['message'].split()[0] not in ['START', 'END', 'REPORT']:
                        if timestamps:
                            print(datetime.datetime.fromtimestamp(log['timestamp'] / 1000), log['message'].replace('\t', ' ').strip(), flush=True)
                        else:
                            print(log['message'].replace('\t', ' ').strip(), flush=True)
