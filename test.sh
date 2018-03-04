#!/bin/bash
set -euo pipefail

cd $(dirname $(realpath $0))

for example in examples/*.py; do
    echo
    echo test: $example
    py.test -s --tb native --doctest-modules $example
done
