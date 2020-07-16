### composable, succinct scripts to complement the aws cli

##### installation

```
git clone https://github.com/nathants/cli-aws
cd cli-aws
python3 -m pip install -r requirements.txt .
```

##### setup

define some environment variables in your bashrc for common default values

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

##### usage and help

- `aws-*-* --help`

- `aws-ec2-ssh --help`

##### bash completion

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

now when you tab complete any cli that takes *selectors, you will see the results of `aws-ec2-ls`

##### examples

- [lambda-api](./examples/lambda/api.py)

- [lambda-basic](./examples/lambda/basic.py)

- [lambda-dependencies](./examples/lambda/dependencies.py)

- [lambda-ec2](./examples/lambda/ec2.py)

- [lambda-includes](./examples/lambda/includes.py)

- [lambda-kms](./examples/lambda/kms.py)

- [lambda-s3](./examples/lambda/s3.py)

- [lambda-scheduled](./examples/lambda/scheduled.py)

- [lambda-sns](./examples/lambda/sns.py)

##### test

`tox`
