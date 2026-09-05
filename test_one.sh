#!/bin/bash
set -eou pipefail
name=${1:?test file stem is required}
cd "$(dirname "$0")/lib"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
read -r -a sources <<< "$(go list -f '{{join .GoFiles " "}}' .)"
read -r -a tests <<< "$(go list -f '{{join .TestGoFiles " "}}' .)"
matched=false
for test in "${tests[@]}"; do
    if [[ "$test" != lib_test.go && "$test" == "${name}_test.go" ]]; then
        matched=true
        go test lib_test.go "${sources[@]}" "$test" -o "$work/libaws.test" -c
        timeout --kill-after=30s 600 "$work/libaws.test" -test.v -test.failfast
    fi
done
if [[ "$matched" == false ]]; then
    echo "no eligible test file: ${name}_test.go" >&2
    exit 1
fi
