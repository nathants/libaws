#!/bin/bash
set -euo pipefail

if ! which aws-lambda-deploy; then
    echo please install cli-aws before running tests: python3 setup.py develop 1>&2
    exit 1
fi

if ! which py.test; then
    echo please install py.test: pip3 install pytest 1>&2
    exit 1
fi

cd $(dirname $(realpath $0))

for example in examples/*.py; do
    echo
    echo test: $example
    py.test -s --tb native --doctest-modules $example
done
