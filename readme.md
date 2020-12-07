# cli-aws

## why

wrangling cloud infra should be both easy and simple.

## what

composable, succinct scripts to complement the aws cli.

## install

```
python -m pip install -v git+https://github.com/nathants/cli-aws
```

## setup

define some environment variables in your bashrc for common default values.

```
export AWS_EMR_SCRIPT_BUCKET=$NAME
export AWS_EC2_KEY=$NAME
export AWS_EC2_AMI=$NAME
export AWS_EC2_SG=$NAME
export AWS_EC2_VPC=NAME
export AWS_EC2_TYPE=i3en.xlarge
export AWS_EC2_SPOT=1.0
export AWS_EC2_TIMEOUT=$((60*60))
```

## discovery

tab complete:

```
aws-
```

## usage

- `aws-*-* --help`

- `aws-ec2-ssh --help`

## bash completion

have something like this in your bashrc:

```
for completion in ~/.completions.d/*.sh; do
    . $completion
done
```

then install the cli-aws completion:

```
./completions.d/install.py ~/completions.d
```

now when you tab complete any cli that takes *selectors, you will see the results of `aws-ec2-ls`.

## examples

- [lambda-api](https://github.com/nathants/cli-aws/blob/master/examples/lambda/api.py)

- [lambda-basic](https://github.com/nathants/cli-aws/blob/master/examples/lambda/basic.py)

- [lambda-dependencies](https://github.com/nathants/cli-aws/blob/master/examples/lambda/dependencies.py)

- [lambda-dynamodb](https://github.com/nathants/cli-aws/blob/master/examples/lambda/dynamodb.py)

- [lambda-ec2](https://github.com/nathants/cli-aws/blob/master/examples/lambda/ec2.py)

- [lambda-includes](https://github.com/nathants/cli-aws/blob/master/examples/lambda/includes.py)

- [lambda-kms](https://github.com/nathants/cli-aws/blob/master/examples/lambda/kms.py)

- [lambda-s3](https://github.com/nathants/cli-aws/blob/master/examples/lambda/s3.py)

- [lambda-scheduled](https://github.com/nathants/cli-aws/blob/master/examples/lambda/scheduled.py)

- [lambda-sns](https://github.com/nathants/cli-aws/blob/master/examples/lambda/sns.py)

- [lambda-sqs](https://github.com/nathants/cli-aws/blob/master/examples/lambda/sqs.py)

## test

```
tox
```
