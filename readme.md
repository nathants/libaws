### composable, succinct scripts to complement the aws cli

##### the successor to [py-aws](https://github.com/nathants/py-aws)

##### installation
```
git clone https://github.com/nathants/cli-aws
cd cli-aws
pip install -r requirements.txt .
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

- [lambda-basic](./examples/lambda_basic.py)

- [lambda-sns](./examples/lambda_sns.py)

- [lambda-scheduled](./examples/lambda_scheduled.py)

- [lambda-kms](./examples/lambda_kms.py)

- [lambda-api](./examples/lambda_api.py)

- [lambda-dependencies](./examples/lambda_dependencies.py)

##### test

`tox`
