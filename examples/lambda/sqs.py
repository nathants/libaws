#!/usr/bin/env python3
#
# conf: concurrency 0
# conf: memory 128
# conf: timeout 60
# policy: AWSLambdaBasicExecutionRole
# sqs: test-sqs
# trigger: sqs test-sqs start=trim_horizon batch=1 parallel=10 retry=5

def main(event, context):
    """

    """

    # >>> import shell, uuid
    # >>> run = lambda *a, **kw: shell.run(*a, stream=True, **kw)
    # >>> path = 'examples/lambda/sqs.py'
    # >>> uid = str(uuid.uuid4())[-12:]
    # >>> _ = run(f'aws-lambda-deploy {path} -y')
    # >>> _ = run(f"aws sqs publish --topic-arn $(aws-lambda-sqs {path}) --message {uid} >/dev/null")
    # >>> assert uid == run(f'aws-lambda-logs {path} -f -e {uid} | tail -n1').split()[-1]
    # >>> _ = run('aws-lambda-rm -ey', path)

    msg = event['Records'][0]['Sqs']['Message']
    print(msg)
