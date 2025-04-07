#!/bin/bash
set -eou pipefail
name=$1
(
    cd lib
    nontest=$(ls *.go | grep -v _test.go)
    for test in $(ls *_test.go | grep -v lib_test.go | grep "${name}_test.go"); do
        go test lib_test.go $nontest $test -o /tmp/libaws.test -c
        timeout 600 /tmp/libaws.test -test.v -test.failfast
    done
)
