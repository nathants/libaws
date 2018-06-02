### cli-aws: composable, succinct aws scripts

##### the successor to [py-aws](https://github.com/nathants/py-aws)

##### installation
`pip-3.6 install --process-dependency-links git+https://github.com/nathants/cli-aws@<git-hash>`

##### usage and help

- `aws-*-* --help`

- `aws-ec2-ssh --help`


##### examples

- [lambda-basic](./examples/lambda_basic.py)

- [lambda-sns](./examples/lambda_sns.py)

- [lambda-scheduled](./examples/lambda_scheduled.py)

- [lambda-kms](./examples/lambda_kms.py)

- [lambda-api](./examples/lambda_api.py)

- [lambda-dependencies](./examples/lambda_dependencies.py)

##### test

`tox`
