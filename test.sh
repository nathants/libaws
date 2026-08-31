#!/bin/bash
set -eou pipefail
make check
make
for dir in examples/simple/python examples/simple/go examples/simple/docker examples/complex examples/misc; do
    (
        cd "$dir"
        for name in *; do
            printf '\n=== %s/%s/test.py ===\n' "$dir" "$name"
            (cd "$name" && timeout 600 uv run --locked python -u test.py)
        done
    )
done
(
    cd lib
    nontest=$(ls *.go | grep -v _test.go)
    for test in $(ls *_test.go | grep -v lib_test.go); do
        printf '\n=== lib/%s ===\n' "$test"
        go test lib_test.go $nontest $test -o /tmp/libaws.test -c
        timeout 600 /tmp/libaws.test -test.v -test.failfast
    done
)
