import aws

def arn(name):
    return f'arn:aws:sns:{aws.region()}:{aws.account()}:{name}'
